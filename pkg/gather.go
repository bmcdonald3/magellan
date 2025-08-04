package magellan

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/OpenCHAMI/magellan/pkg/bmc"
	"github.com/stmcginnis/gofish"
	"github.com/stmcginnis/gofish/common"
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
	FRUID                 string                 `json:"FRUID"`
	Type                  string                 `json:"Type"`
	HWInventoryByFRUType  string                 `json:"HWInventoryByFRUType"`
	ProcessorFRUInfo      *ProcessorFRUInfo      `json:"ProcessorFRUInfo,omitempty"`
	MemoryFRUInfo         *MemoryFRUInfo         `json:"MemoryFRUInfo,omitempty"`
	DriveFRUInfo          *DriveFRUInfo          `json:"DriveFRUInfo,omitempty"`
	NetworkAdapterFRUInfo *NetworkAdapterFRUInfo `json:"NetworkAdapterFRUInfo,omitempty"`
	PSUFRUInfo            *PSUFRUInfo            `json:"PSUFRUInfo,omitempty"`
	AcceleratorFRUInfo    *AcceleratorFRUInfo    `json:"AcceleratorFRUInfo,omitempty"`
}

// ProcessorFRUInfo contains descriptive metadata for a CPU.
type ProcessorFRUInfo struct {
	Manufacturer string `json:"Manufacturer,omitempty"`
	Model        string `json:"Model,omitempty"`
	TotalCores   int    `json:"TotalCores,omitempty"`
}

// AcceleratorFRUInfo contains descriptive metadata for a GPU/Accelerator.
type AcceleratorFRUInfo struct {
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

// DriveFRUInfo contains descriptive metadata for a storage drive.
type DriveFRUInfo struct {
	Manufacturer  string `json:"Manufacturer,omitempty"`
	Model         string `json:"Model,omitempty"`
	PartNumber    string `json:"PartNumber,omitempty"`
	SerialNumber  string `json:"SerialNumber,omitempty"`
	CapacityBytes int64  `json:"CapacityBytes,omitempty"`
}

// NetworkAdapterFRUInfo contains descriptive metadata for a NIC.
type NetworkAdapterFRUInfo struct {
	Manufacturer string `json:"Manufacturer,omitempty"`
	Model        string `json:"Model,omitempty"`
	PartNumber   string `json:"PartNumber,omitempty"`
	SerialNumber string `json:"SerialNumber,omitempty"`
}

// PSUFRUInfo contains descriptive metadata for a Power Supply Unit.
type PSUFRUInfo struct {
	Manufacturer       string  `json:"Manufacturer,omitempty"`
	Model              string  `json:"Model,omitempty"`
	PartNumber         string  `json:"PartNumber,omitempty"`
	SerialNumber       string  `json:"SerialNumber,omitempty"`
	PowerCapacityWatts float32 `json:"PowerCapacityWatts,omitempty"`
}

// GatherFRUInventory connects to a list of BMCs, gathers detailed FRU data,
// transforms it into the SMD format, and prints it to standard output.
func GatherFRUInventory(hosts []string, params *CollectParams) error {
	var allHardware []HWInventoryByLocation

	tr := &http.Transport{
		Proxy:           nil,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	httpClient := &http.Client{
		Transport: tr,
		Timeout:   30 * time.Second,
	}

	for _, host := range hosts {
		creds, err := bmc.GetBMCCredentials(params.SecretStore, host)
		if err != nil {
			return fmt.Errorf("failed to get credentials for %s: %v", host, err)
		}

		config := gofish.ClientConfig{
			Endpoint:   "https://" + host,
			Username:   creds.Username,
			Password:   creds.Password,
			Insecure:   true,
			HTTPClient: httpClient,
		}

		client, err := gofish.Connect(config)
		if err != nil {
			return fmt.Errorf("failed to connect to %s: %v", host, err)
		}
		defer client.Logout()

		chassisList, err := client.GetService().Chassis()
		if err != nil {
			return fmt.Errorf("failed to get chassis list from %s: %v", host, err)
		}

		for _, chassis := range chassisList {
			power, _ := chassis.Power()
			if power != nil {
				// FIX: Capture the index 'i' to create the ordinal xname ID.
				for i, psu := range power.PowerSupplies {
					if psu.Status.State == common.EnabledState {
						// FIX: Pass the index 'i' to the transform function.
						fru := transformPSU(&psu, chassis.ID, i)
						allHardware = append(allHardware, fru)
					}
				}
			}

			systems, _ := chassis.ComputerSystems()
			for _, system := range systems {
				processors, _ := system.Processors()
				// FIX: Capture the index 'i' to create the ordinal xname ID.
				for i, proc := range processors {
					if proc.Status.State == common.EnabledState {
						if proc.ProcessorType == "CPU" {
							// FIX: Pass the index 'i' to the transform function.
							fru := transformProcessor(proc, system.ID, i)
							allHardware = append(allHardware, fru)
						} else if proc.ProcessorType == "GPU" || proc.ProcessorType == "Accelerator" {
							// FIX: Pass the index 'i' to the transform function.
							fru := transformAccelerator(proc, system.ID, i)
							allHardware = append(allHardware, fru)
						}
					}
				}

				memoryModules, _ := system.Memory()
				// FIX: Capture the index 'i' to create the ordinal xname ID.
				for i, mem := range memoryModules {
					if mem.Status.State == common.EnabledState {
						// FIX: Pass the index 'i' to the transform function.
						fru := transformMemory(mem, system.ID, i)
						allHardware = append(allHardware, fru)
					}
				}

				storageControllers, _ := system.Storage()
				for _, storage := range storageControllers {
					drives, _ := storage.Drives()
					// FIX: Capture the index 'i' to create the ordinal xname ID.
					for i, drive := range drives {
						if drive.Status.State == common.EnabledState {
							// FIX: Pass the index 'i' to the transform function.
							fru := transformDrive(drive, system.ID, storage.ID, i)
							allHardware = append(allHardware, fru)
						}
					}
				}

				netInterfaces, _ := system.NetworkInterfaces()
				// FIX: Capture the index 'i' to create the ordinal xname ID.
				for i, nic := range netInterfaces {
					adapter, _ := nic.NetworkAdapter()
					if adapter != nil {
						// FIX: Pass the index 'i' to the transform function.
						fru := transformNetworkAdapter(adapter, system.ID, i)
						allHardware = append(allHardware, fru)
					}
				}
			}
		}
	}

	output, err := json.MarshalIndent(allHardware, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal final payload: %v", err)
	}

	fmt.Fprintln(os.Stdout, string(output))
	return nil
}

// FIX: Add index parameter to construct the xname ID correctly.
func transformProcessor(proc *redfish.Processor, nodeXname string, index int) HWInventoryByLocation {
	manufacturer := strings.TrimSpace(proc.Manufacturer)
	model := strings.TrimSpace(proc.Model)
	serial := strings.TrimSpace(proc.SerialNumber)
	socket := strings.TrimSpace(proc.Socket)
	socketID := strings.ReplaceAll(socket, " ", "")

	fruid := fmt.Sprintf("%s-%s-%s", manufacturer, model, serial)
	if serial == "" {
		fruid = fmt.Sprintf("%s-%s-%s", manufacturer, model, socketID)
	}

	return HWInventoryByLocation{
		ID:                        fmt.Sprintf("%sp%d", strings.ToLower(nodeXname), index),
		Type:                      "Processor",
		Status:                    "Populated",
		HWInventoryByLocationType: "HWInvByLocProcessor",
		PopulatedFRU: &HWInventoryByFRU{
			FRUID:                fruid,
			Type:                 "Processor",
			HWInventoryByFRUType: "HWInvByFRUProcessor",
			ProcessorFRUInfo: &ProcessorFRUInfo{
				Manufacturer: manufacturer,
				Model:        model,
				TotalCores:   proc.TotalCores,
			},
		},
	}
}

// FIX: Add index parameter to construct the xname ID correctly.
func transformAccelerator(proc *redfish.Processor, nodeXname string, index int) HWInventoryByLocation {
	manufacturer := strings.TrimSpace(proc.Manufacturer)
	model := strings.TrimSpace(proc.Model)
	serial := strings.TrimSpace(proc.SerialNumber)
	socket := strings.TrimSpace(proc.Socket)
	socketID := strings.ReplaceAll(socket, " ", "")

	fruid := fmt.Sprintf("%s-%s-%s", manufacturer, model, serial)
	if serial == "" {
		fruid = fmt.Sprintf("%s-%s-%s", manufacturer, model, socketID)
	}

	return HWInventoryByLocation{
		ID:                        fmt.Sprintf("%sa%d", strings.ToLower(nodeXname), index),
		Type:                      "NodeAccel",
		Status:                    "Populated",
		HWInventoryByLocationType: "HWInvByLocNodeAccel",
		PopulatedFRU: &HWInventoryByFRU{
			FRUID:                fruid,
			Type:                 "NodeAccel",
			HWInventoryByFRUType: "HWInvByFRUNodeAccel",
			AcceleratorFRUInfo: &AcceleratorFRUInfo{
				Manufacturer: manufacturer,
				Model:        model,
			},
		},
	}
}

// FIX: Add index parameter to construct the xname ID correctly.
func transformMemory(mem *redfish.Memory, nodeXname string, index int) HWInventoryByLocation {
	manufacturer := strings.TrimSpace(mem.Manufacturer)
	partNumber := strings.TrimSpace(mem.PartNumber)
	serial := strings.TrimSpace(mem.SerialNumber)

	fruid := fmt.Sprintf("%s-%s-%s", manufacturer, partNumber, serial)

	return HWInventoryByLocation{
		ID:                        fmt.Sprintf("%sd%d", strings.ToLower(nodeXname), index),
		Type:                      "Memory",
		Status:                    "Populated",
		HWInventoryByLocationType: "HWInvByLocMemory",
		PopulatedFRU: &HWInventoryByFRU{
			FRUID:                fruid,
			Type:                 "Memory",
			HWInventoryByFRUType: "HWInvByFRUMemory",
			MemoryFRUInfo: &MemoryFRUInfo{
				Manufacturer:     manufacturer,
				PartNumber:       partNumber,
				SerialNumber:     serial,
				CapacityMiB:      mem.CapacityMiB,
				MemoryDeviceType: string(mem.MemoryDeviceType),
			},
		},
	}
}

// FIX: Add index parameter to construct the xname ID correctly.
func transformDrive(drive *redfish.Drive, nodeXname, storageID string, index int) HWInventoryByLocation {
	manufacturer := strings.TrimSpace(drive.Manufacturer)
	model := strings.TrimSpace(drive.Model)
	partNumber := strings.TrimSpace(drive.PartNumber)
	serial := strings.TrimSpace(drive.SerialNumber)

	fruid := fmt.Sprintf("%s-%s-%s", manufacturer, model, serial)

	return HWInventoryByLocation{
		ID:                        fmt.Sprintf("%ss%dd%d", strings.ToLower(nodeXname), 0, index), // Assuming storage controller 0, drive index
		Type:                      "Drive",
		Status:                    "Populated",
		HWInventoryByLocationType: "HWInvByLocDrive",
		PopulatedFRU: &HWInventoryByFRU{
			FRUID:                fruid,
			Type:                 "Drive",
			HWInventoryByFRUType: "HWInvByFRUDrive",
			DriveFRUInfo: &DriveFRUInfo{
				Manufacturer:  manufacturer,
				Model:         model,
				PartNumber:    partNumber,
				SerialNumber:  serial,
				CapacityBytes: drive.CapacityBytes,
			},
		},
	}
}

// FIX: Add index parameter to construct the xname ID correctly.
func transformNetworkAdapter(adapter *redfish.NetworkAdapter, nodeXname string, index int) HWInventoryByLocation {
	manufacturer := strings.TrimSpace(adapter.Manufacturer)
	model := strings.TrimSpace(adapter.Model)
	partNumber := strings.TrimSpace(adapter.PartNumber)
	serial := strings.TrimSpace(adapter.SerialNumber)

	fruid := fmt.Sprintf("%s-%s-%s", manufacturer, partNumber, serial)

	return HWInventoryByLocation{
		ID:                        fmt.Sprintf("%sn%d", strings.ToLower(nodeXname), index),
		Type:                      "NodeHsnNic",
		Status:                    "Populated",
		HWInventoryByLocationType: "HWInvByLocHSNNIC",
		PopulatedFRU: &HWInventoryByFRU{
			FRUID:                fruid,
			Type:                 "NodeHsnNic",
			HWInventoryByFRUType: "HWInvByFRUHSNNIC",
			NetworkAdapterFRUInfo: &NetworkAdapterFRUInfo{
				Manufacturer: manufacturer,
				Model:        model,
				PartNumber:   partNumber,
				SerialNumber: serial,
			},
		},
	}
}

// FIX: Add index parameter to construct the xname ID correctly.
func transformPSU(psu *redfish.PowerSupply, chassisXname string, index int) HWInventoryByLocation {
	manufacturer := strings.TrimSpace(psu.Manufacturer)
	model := strings.TrimSpace(psu.Model)
	partNumber := strings.TrimSpace(psu.PartNumber)
	serial := strings.TrimSpace(psu.SerialNumber)

	fruid := fmt.Sprintf("%s-%s-%s", manufacturer, model, serial)

	return HWInventoryByLocation{
		ID:                        fmt.Sprintf("%sps%d", strings.ToLower(chassisXname), index),
		Type:                      "NodeEnclosurePowerSupply",
		Status:                    "Populated",
		HWInventoryByLocationType: "HWInvByLocNodeEnclosurePowerSupply",
		PopulatedFRU: &HWInventoryByFRU{
			FRUID:                fruid,
			Type:                 "NodeEnclosurePowerSupply",
			HWInventoryByFRUType: "HWInvByFRUNodeEnclosurePowerSupply",
			PSUFRUInfo: &PSUFRUInfo{
				Manufacturer:       manufacturer,
				Model:              model,
				PartNumber:         partNumber,
				SerialNumber:       serial,
				PowerCapacityWatts: psu.PowerCapacityWatts,
			},
		},
	}
}
