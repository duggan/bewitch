package collector

// SAT (SCSI-ATA Translation) fallback for reading SMART data from SATA drives
// whose kernel reports a non-standard vendor ident (e.g., null-padded "ATA\x00"
// instead of space-padded "ATA     "), causing the smart.go library's OpenSata()
// to reject them.
//
// This implements just enough of the ATA-over-SCSI passthrough to read the
// SMART attribute page and extract the fields we care about.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	sgIO              = 0x2285
	sgDxferFromDev    = -3
	sgInfoOkMask      = 0x1
	sgInfoOk          = 0x0
	sgTimeout         = 20000 // ms
	scsiInquiryCmd    = 0x12
	scsiATAPassthru16 = 0x85
	ataIdentifyDevice = 0xec
	ataSMART          = 0xb0
	smartReadData     = 0xd0
)

type sgIoHdr struct {
	interfaceID    int32
	dxferDirection int32
	cmdLen         uint8
	mxSbLen        uint8
	iovecCount     uint16
	dxferLen       uint32
	dxferp         uintptr
	cmdp           uintptr
	sbp            uintptr
	timeout        uint32
	flags          uint32
	packID         int32
	usrPtr         uintptr
	status         uint8
	maskedStatus   uint8
	msgStatus      uint8
	sbLenWr        uint8
	hostStatus     uint16
	driverStatus   uint16
	resid          int32
	duration       uint32
	info           uint32
}

func sgSendCDB(fd int, cdb []byte, resp []byte) error {
	sense := make([]byte, 32)
	hdr := sgIoHdr{
		interfaceID:    'S',
		dxferDirection: sgDxferFromDev,
		timeout:        sgTimeout,
		cmdLen:         uint8(len(cdb)),
		mxSbLen:        uint8(len(sense)),
		dxferLen:       uint32(len(resp)),
		dxferp:         uintptr(unsafe.Pointer(&resp[0])),
		cmdp:           uintptr(unsafe.Pointer(&cdb[0])),
		sbp:            uintptr(unsafe.Pointer(&sense[0])),
	}
	if err := ioctl(uintptr(fd), sgIO, uintptr(unsafe.Pointer(&hdr))); err != nil {
		return err
	}
	if hdr.info&sgInfoOkMask != sgInfoOk {
		return fmt.Errorf("SG_IO status: device=%#02x host=%#02x driver=%#02x",
			hdr.status, hdr.hostStatus, hdr.driverStatus)
	}
	return nil
}

func ioctl(fd, cmd, arg uintptr) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, cmd, arg)
	if errno != 0 {
		return errno
	}
	return nil
}

// isATADevice opens the device, sends a SCSI Inquiry, and returns true if the
// trimmed vendor ident starts with "ATA". This catches both space-padded and
// null-padded variants.
func isATADevice(fd int) bool {
	resp := make([]byte, 36)
	cdb := [6]byte{scsiInquiryCmd}
	binary.BigEndian.PutUint16(cdb[3:5], uint16(len(resp)))
	if err := sgSendCDB(fd, cdb[:], resp); err != nil {
		return false
	}
	// Vendor ident is bytes 8..15
	vendor := resp[8:16]
	trimmed := bytes.TrimRight(vendor, " \x00")
	return bytes.Equal(trimmed, []byte("ATA"))
}

// satSmartAttrRaw is one raw SMART attribute entry (12 bytes).
type satSmartAttrRaw struct {
	ID          uint8
	Flags       uint16
	Current     uint8
	Worst       uint8
	VendorBytes [6]byte
	Reserved    uint8
}

// satSmartPage is the raw SMART data page (362 bytes used out of 512).
type satSmartPage struct {
	Version uint16
	Attrs   [30]satSmartAttrRaw
}

// satSMARTInfo reads SMART data via ATA passthrough and returns a SMARTInfo.
// Only call this after confirming the device is ATA via isATADevice().
func satSMARTInfo(devPath string) (*SMARTInfo, error) {
	fd, err := unix.Open(devPath, unix.O_RDONLY, 0o600)
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)

	if !isATADevice(fd) {
		return nil, fmt.Errorf("not an ATA device")
	}

	info := &SMARTInfo{Available: true, Healthy: true}

	// Read SMART DATA via ATA passthrough
	smartBuf := make([]byte, 512)
	cdb := [16]byte{scsiATAPassthru16}
	cdb[1] = 0x08         // PIO data-in
	cdb[2] = 0x0e         // BYT_BLOK=1, T_LENGTH=2, T_DIR=1
	cdb[4] = smartReadData // feature
	cdb[10] = 0x4f        // lba_mid
	cdb[12] = 0xc2        // lba_high
	cdb[14] = ataSMART     // command
	if err := sgSendCDB(fd, cdb[:], smartBuf); err != nil {
		return nil, fmt.Errorf("SMART READ DATA: %w", err)
	}

	var page satSmartPage
	if err := binary.Read(bytes.NewReader(smartBuf[:362]), binary.LittleEndian, &page); err != nil {
		return nil, fmt.Errorf("parsing SMART page: %w", err)
	}

	for _, a := range page.Attrs {
		if a.ID == 0 {
			break
		}
		raw := rawValue48(a.VendorBytes)
		switch a.ID {
		case 1: // Raw_Read_Error_Rate
			info.ReadErrorRate = raw
		case 5: // Reallocated_Sector_Ct
			info.ReallocatedSectors = raw
		case 9: // Power_On_Hours
			info.PowerOnHours = raw
		case 12: // Power_Cycle_Count
			info.PowerCycles = raw
		case 190, 194: // Temperature_Celsius / Airflow_Temperature_Cel
			info.Temperature = raw & 0xff // low byte is current temp
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

	info.Healthy = info.ReallocatedSectors == 0 && info.PendingSectors == 0 && info.UncorrectableErrs == 0
	return info, nil
}

// rawValue48 extracts a 48-bit raw value from the 6 vendor bytes (little-endian).
func rawValue48(b [6]byte) uint64 {
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 |
		uint64(b[3])<<24 | uint64(b[4])<<32 | uint64(b[5])<<40
}
