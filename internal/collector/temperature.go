package collector

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"
)


type TempSensorSample struct {
	Sensor      string
	TempCelsius float64
}

type TemperatureData struct {
	Sensors []TempSensorSample
}

type sensorPath struct {
	inputPath string
	labelPath string
	name      string
}

type TemperatureCollector struct {
	sensors  []sensorPath
	sysfsCache
}

func NewTemperatureCollector() *TemperatureCollector {
	c := &TemperatureCollector{
		sensors: make([]sensorPath, 0, 16),
	}
	c.discoverSensors()
	return c
}

func (c *TemperatureCollector) Name() string { return "temperature" }

func (c *TemperatureCollector) discoverSensors() {
	c.sensors = c.sensors[:0]

	// Check hwmon sensors first
	hwmonDirs, _ := filepath.Glob("/sys/class/hwmon/hwmon*")
	for _, dir := range hwmonDirs {
		name := readString(filepath.Join(dir, "name"))
		if name == "" {
			name = filepath.Base(dir)
		}
		tempInputs, _ := filepath.Glob(filepath.Join(dir, "temp*_input"))
		for _, input := range tempInputs {
			c.sensors = append(c.sensors, sensorPath{
				inputPath: input,
				labelPath: strings.Replace(input, "_input", "_label", 1),
				name:      name,
			})
		}
	}

	// Fallback: thermal zones if no hwmon sensors
	if len(c.sensors) == 0 {
		tzDirs, _ := filepath.Glob("/sys/class/thermal/thermal_zone*")
		for _, dir := range tzDirs {
			tzType := readString(filepath.Join(dir, "type"))
			if tzType == "" {
				tzType = filepath.Base(dir)
			}
			c.sensors = append(c.sensors, sensorPath{
				inputPath: filepath.Join(dir, "temp"),
				name:      tzType,
			})
		}
	}

	c.markRefreshed()
}

func (c *TemperatureCollector) Collect() (Sample, error) {
	if c.needsRefresh(len(c.sensors)) {
		c.discoverSensors()
	}

	sensors := make([]TempSensorSample, 0, len(c.sensors))
	for _, sp := range c.sensors {
		milliC, err := strconv.ParseInt(strings.TrimSpace(readStringFile(sp.inputPath)), 10, 64)
		if err != nil {
			continue
		}

		sensorName := sp.name
		if sp.labelPath != "" {
			if label := readString(sp.labelPath); label != "" {
				sensorName = sp.name + "/" + label
			}
		}

		sensors = append(sensors, TempSensorSample{
			Sensor:      sensorName,
			TempCelsius: float64(milliC) / 1000.0,
		})
	}

	return Sample{
		Timestamp: time.Now(),
		Kind:      "temperature",
		Data:      TemperatureData{Sensors: sensors},
	}, nil
}
