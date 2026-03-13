package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// smartctl JSON output structures — only fields we need are defined.
// Unknown keys are silently ignored by encoding/json.

type smartctlOutput struct {
	Smartctl    smartctlMeta                `json:"smartctl"`
	Device      smartctlDevice              `json:"device"`
	SmartStatus smartctlSmartStatus         `json:"smart_status"`
	Temperature *smartctlTemperature        `json:"temperature"`
	ATASmart    *smartctlATASmartAttrs      `json:"ata_smart_attributes"`
	NVMEHealth  *smartctlNVMEHealthInfoLog  `json:"nvme_smart_health_information_log"`
}

type smartctlMeta struct {
	ExitStatus int              `json:"exit_status"`
	Messages   []smartctlMsg    `json:"messages"`
}

type smartctlMsg struct {
	String   string `json:"string"`
	Severity string `json:"severity"`
}

type smartctlDevice struct {
	Type string `json:"type"`
}

type smartctlSmartStatus struct {
	Passed *bool `json:"passed"`
}

type smartctlTemperature struct {
	Current int `json:"current"`
}

type smartctlATASmartAttrs struct {
	Table []smartctlATAAttr `json:"table"`
}

type smartctlATAAttr struct {
	ID  int               `json:"id"`
	Raw smartctlATARawVal `json:"raw"`
}

type smartctlATARawVal struct {
	Value int64 `json:"value"`
}

type smartctlNVMEHealthInfoLog struct {
	CriticalWarning  int   `json:"critical_warning"`
	Temperature      int   `json:"temperature"`
	AvailableSpare   int   `json:"available_spare"`
	PercentageUsed   int   `json:"percentage_used"`
	PowerCycles      int64 `json:"power_cycles"`
	PowerOnHours     int64 `json:"power_on_hours"`
	DataUnitsRead    int64 `json:"data_units_read"`
	DataUnitsWritten int64 `json:"data_units_written"`
	MediaErrors      int64 `json:"media_errors"`
}

// detectSmartctl checks if smartctl is available on the system.
// Returns the full path if found, empty string otherwise.
func detectSmartctl() string {
	path, err := exec.LookPath("smartctl")
	if err != nil {
		return ""
	}
	return path
}

// readSMARTFromSmartctl runs smartctl on the given device and parses the JSON
// output into a SMARTInfo. It first tries auto-detection, then retries with
// -d sat for USB-bridged drives that smartctl can't auto-detect.
func readSMARTFromSmartctl(smartctlPath, devPath string) (*SMARTInfo, error) {
	info, err := runSmartctl(smartctlPath, devPath, "")
	if err == nil {
		return info, nil
	}

	// If the error mentions "Unknown USB bridge", retry with -d sat.
	// Many USB-to-SATA adapters work fine with SAT passthrough but have
	// unrecognized USB vendor/product IDs.
	if strings.Contains(err.Error(), "Unknown USB bridge") {
		if info, satErr := runSmartctl(smartctlPath, devPath, "sat"); satErr == nil {
			return info, nil
		}
	}

	return nil, err
}

// runSmartctl executes smartctl -j -a and parses the result. If devType is
// non-empty, it is passed as -d <devType>.
func runSmartctl(smartctlPath, devPath, devType string) (*SMARTInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	args := []string{"-j", "-a"}
	if devType != "" {
		args = append(args, "-d", devType)
	}
	args = append(args, devPath)

	cmd := exec.CommandContext(ctx, smartctlPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()

	// smartctl uses exit-status bit flags. Bits 0-1 are command-line /
	// permission errors. Bits 2+ indicate device warnings (not fatal for
	// our purposes). If we got JSON output, try to parse regardless.
	if len(out) == 0 {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("smartctl failed for %s: %s", devPath, strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("smartctl produced no output for %s: %w", devPath, err)
	}

	var result smartctlOutput
	if jsonErr := json.Unmarshal(out, &result); jsonErr != nil {
		return nil, fmt.Errorf("smartctl JSON parse error for %s: %w", devPath, jsonErr)
	}

	// Bits 0-1 set AND no useful health data → fall back.
	if result.Smartctl.ExitStatus&0x3 != 0 {
		if result.NVMEHealth == nil && result.ATASmart == nil {
			msg := smartctlErrorMsg(result.Smartctl.Messages)
			if msg != "" {
				return nil, fmt.Errorf("smartctl: %s", msg)
			}
			return nil, fmt.Errorf("smartctl error (exit %d) for %s",
				result.Smartctl.ExitStatus, devPath)
		}
	}

	switch result.Device.Type {
	case "nvme":
		return parseSmartctlNVMe(&result), nil
	case "ata", "sat":
		return parseSmartctlATA(&result), nil
	default:
		// Infer from which health data is present.
		if result.NVMEHealth != nil {
			return parseSmartctlNVMe(&result), nil
		}
		if result.ATASmart != nil {
			return parseSmartctlATA(&result), nil
		}
		return nil, fmt.Errorf("smartctl: no health data for %s (device type %q)",
			devPath, result.Device.Type)
	}
}

// smartctlErrorMsg returns the first error-severity message from smartctl output,
// or empty string if none.
func smartctlErrorMsg(msgs []smartctlMsg) string {
	for _, m := range msgs {
		if m.Severity == "error" {
			return m.String
		}
	}
	return ""
}

func parseSmartctlNVMe(out *smartctlOutput) *SMARTInfo {
	info := &SMARTInfo{Available: true, Healthy: true}

	if h := out.NVMEHealth; h != nil {
		info.Healthy = h.CriticalWarning == 0
		info.Temperature = uint64(h.Temperature)
		info.AvailableSpare = uint8(h.AvailableSpare)
		info.PercentUsed = uint8(h.PercentageUsed)
		info.PowerCycles = uint64(h.PowerCycles)
		info.PowerOnHours = uint64(h.PowerOnHours)
		// smartctl reports data units as 1000 × 512-byte sectors.
		info.ReadSectors = uint64(h.DataUnitsRead) * 1000
		info.WrittenSectors = uint64(h.DataUnitsWritten) * 1000
		info.UncorrectableErrs = uint64(h.MediaErrors)
	}

	if info.Temperature == 0 && out.Temperature != nil {
		info.Temperature = uint64(out.Temperature.Current)
	}

	return info
}

func parseSmartctlATA(out *smartctlOutput) *SMARTInfo {
	info := &SMARTInfo{Available: true}

	if out.Temperature != nil {
		info.Temperature = uint64(out.Temperature.Current)
	}

	if out.ATASmart != nil {
		for _, attr := range out.ATASmart.Table {
			raw := uint64(attr.Raw.Value)
			switch attr.ID {
			case 1: // Raw_Read_Error_Rate
				info.ReadErrorRate = raw
			case 5: // Reallocated_Sector_Ct
				info.ReallocatedSectors = raw
			case 9: // Power_On_Hours
				info.PowerOnHours = raw
			case 12: // Power_Cycle_Count
				info.PowerCycles = raw
			case 190, 194: // Temperature_Celsius / Airflow_Temperature_Cel
				if info.Temperature == 0 {
					info.Temperature = raw & 0xff
				}
			case 197: // Current_Pending_Sector
				info.PendingSectors = raw
			case 198: // Offline_Uncorrectable
				info.UncorrectableErrs = raw
			case 241: // Total_LBAs_Written
				info.WrittenSectors = raw
			case 242: // Total_LBAs_Read
				info.ReadSectors = raw
			}
		}
	}

	// Use smartctl's SMART RETURN STATUS when present. If absent (e.g.,
	// USB-bridged drives where the command couldn't be issued), derive
	// health from error counters.
	if out.SmartStatus.Passed != nil {
		info.Healthy = *out.SmartStatus.Passed
	} else {
		info.Healthy = info.ReallocatedSectors == 0 &&
			info.PendingSectors == 0 &&
			info.UncorrectableErrs == 0
	}

	return info
}
