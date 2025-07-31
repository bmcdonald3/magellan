package magellan

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/OpenCHAMI/magellan/pkg/bmc"
	"github.com/stmcginnis/gofish"
	"github.com/stmcginnis/gofish/common" // FIX: Add this import
	"github.com/stmcginnis/gofish/redfish"
)

// SMDHardwarePayload is the top-level object for the SMD /Inventory/Hardware endpoint.
type SMDHardwarePayload struct {
	Hardware []HWInventoryByLocation `json:"Hardware"`
}

// HWInventoryByLocation represents a physical slot in the system and the FRU it contains.
type HWInventoryByLocation struct {
	ID                        string            `json:"ID"`
	Type                      string            `json:"Type"`
	Status                    string            `json:"Status"`
	HWInventoryByLocationType string            `json:"HWInventoryByLocationType"`
	PopulatedFRU              *HWInventoryByFRU `json:"PopulatedFRU,omitempty"`
}

// HWInventoryByFRU represents the actual physical piece of hardware.
type HWInventoryByFRU struct {
	FRUID                string            `json:"FRUID"`
	Type                 string            `json:"Type"`
	HWInventoryByFRUType string            `json:"HWInventoryByFRUType"`
	ProcessorFRUInfo     *ProcessorFRUInfo `json:"ProcessorFRUInfo,omitempty"`
	MemoryFRUInfo        *MemoryFRUInfo    `json:"MemoryFRUInfo,omitempty"`
}

// ProcessorFRUInfo contains descriptive metadata for a CPU.
type ProcessorFRUInfo struct {
	Manufacturer string `json:"Manufacturer,omitempty"`
	Model        string `json:"Model,omitempty"`
	TotalCores   int    `json:"TotalCores,omitempty"`
}

// MemoryFRUInfo contains descriptive metadata for a DIMM.
type MemoryFRUInfo struct {
	Manufacturer     string `json:"Manufacturer,omitempty"`
	PartNumber       string `json:"PartNumber,omitempty"`
	SerialNumber     string `json:"SerialNumber,omitempty"`
	CapacityMiB      int    `json:"CapacityMiB,omitempty"`
	MemoryDeviceType string `json:"MemoryDeviceType,omitempty"`
}

// GatherFRUInventory connects to a list of BMCs, gathers detailed FRU data,
// transforms it into the SMD format, and prints it to standard output.
func GatherFRUInventory(hosts []string, params *CollectParams) error {
	var allHardware []HWInventoryByLocation

	for _, host := range hosts {
		// 1. Get credentials and connect to the BMC
		creds, err := bmc.GetBMCCredentials(params.SecretStore, host)
		if err != nil {
			return fmt.Errorf("failed to get credentials for %s: %v", host, err)
		}

		config := gofish.ClientConfig{
			Endpoint: "https://" + host,
			Username: creds.Username,
			Password: creds.Password,
			Insecure: true,
		}

		client, err := gofish.Connect(config)
		if err != nil {
			return fmt.Errorf("failed to connect to %s: %v", host, err)
		}
		defer client.Logout()

		systems, err := client.GetService().Systems()
		if err != nil {
			return fmt.Errorf("failed to get systems from %s: %v", host, err)
		}

		// 2. Gather Raw FRU Data from each system managed by the BMC
		for _, system := range systems {
			// Get Processors
			processors, err := system.Processors()
			if err == nil {
				for _, proc := range processors {
					// FIX: The correct constant is common.EnabledState.
					if proc.Status.State == common.EnabledState {
						fru := transformProcessor(proc, system.ID)
						allHardware = append(allHardware, fru)
					}
				}
			}

			// Get Memory
			memoryModules, err := system.Memory()
			if err == nil {
				for _, mem := range memoryModules {
					// FIX: The correct constant is common.EnabledState.
					if mem.Status.State == common.EnabledState {
						fru := transformMemory(mem, system.ID)
						allHardware = append(allHardware, fru)
					}
				}
			}
		}
	}

	// 3. Create Final Payload and Print to Stdout
	payload := SMDHardwarePayload{Hardware: allHardware}
	output, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal final payload: %v", err)
	}

	fmt.Fprintln(os.Stdout, string(output))

	return nil
}

// transformProcessor converts a gofish Processor object to an SMD-compatible struct
func transformProcessor(proc *redfish.Processor, nodeXname string) HWInventoryByLocation {
	// Construct a unique FRUID from identifying information
	fruid := fmt.Sprintf("%s-%s-%s", proc.Manufacturer, proc.Model, proc.SerialNumber)
	if proc.SerialNumber == "" {
		// Fallback if serial is missing, as noted in the swagger spec
		fruid = fmt.Sprintf("%s-%s-%s", proc.Manufacturer, proc.Model, proc.Socket)
	}

	return HWInventoryByLocation{
		ID:                        fmt.Sprintf("%sp%s", nodeXname, proc.Socket),
		Type:                      "Processor",
		Status:                    "Populated",
		HWInventoryByLocationType: "HWInvByLocProcessor",
		PopulatedFRU: &HWInventoryByFRU{
			FRUID:                fruid,
			Type:                 "Processor",
			HWInventoryByFRUType: "HWInvByFRUProcessor",
			ProcessorFRUInfo: &ProcessorFRUInfo{
				Manufacturer: proc.Manufacturer,
				Model:        proc.Model,
				TotalCores:   proc.TotalCores,
			},
		},
	}
}

// transformMemory converts a gofish Memory object to an SMD-compatible struct
func transformMemory(mem *redfish.Memory, nodeXname string) HWInventoryByLocation {
	// Construct a unique FRUID from identifying information
	fruid := fmt.Sprintf("%s-%s-%s", mem.Manufacturer, mem.PartNumber, mem.SerialNumber)

	return HWInventoryByLocation{
		ID:                        fmt.Sprintf("%sd%s", nodeXname, mem.DeviceLocator),
		Type:                      "Memory",
		Status:                    "Populated",
		HWInventoryByLocationType: "HWInvByLocMemory",
		PopulatedFRU: &HWInventoryByFRU{
			FRUID:                fruid,
			Type:                 "Memory",
			HWInventoryByFRUType: "HWInvByFRUMemory",
			MemoryFRUInfo: &MemoryFRUInfo{
				Manufacturer:     mem.Manufacturer,
				PartNumber:       mem.PartNumber,
				SerialNumber:     mem.SerialNumber,
				CapacityMiB:      mem.CapacityMiB,
				MemoryDeviceType: string(mem.MemoryDeviceType),
			},
		},
	}
}
