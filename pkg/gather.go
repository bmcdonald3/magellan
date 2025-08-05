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

// RedfishProcessorLocationInfo describes the physical slot for a processor.
type RedfishProcessorLocationInfo struct {
	Socket string `json:"Socket,omitempty"`
}

// RedfishMemoryLocationInfo describes the physical slot for a memory DIMM.
type RedfishMemoryLocationInfo struct {
	Socket           int `json:"Socket,omitempty"`
	MemoryController int `json:"MemoryController,omitempty"`
	Channel          int `json:"Channel,omitempty"`
	Slot             int `json:"Slot,omitempty"`
}

// RedfishDriveLocationInfo describes the physical slot for a drive.
type RedfishDriveLocationInfo struct{}

// RedfishNetworkAdapterLocationInfo describes the physical slot for a NIC.
type RedfishNetworkAdapterLocationInfo struct{}

// RedfishPSULocationInfo describes the physical slot for a PSU.
type RedfishPSULocationInfo struct{}

// HWInventoryByLocation represents a physical slot in the system and the FRU it contains.
type HWInventoryByLocation struct {
	ID                        string            `json:"ID"`
	Type                      string            `json:"Type"`
	Status                    string            `json:"Status"`
	HWInventoryByLocationType string            `json:"HWInventoryByLocationType"`
	PopulatedFRU              *HWInventoryByFRU `json:"PopulatedFRU,omitempty"`

	// This map will be "inlined" into the HWInventoryByLocation JSON object.
	// It will hold the dynamically named location info, e.g., {"CMMRectifierLocationInfo": {...}}.
	LocationInfo map[string]interface{} `json:",inline"`
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

// GatherFRUInventory now builds xnames based on discovery order and known patterns.
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

	// This prototype will process the first host and assume it belongs to a test cabinet (e.g., x3000)
	host := hosts[0]
	const cabinetID = 3000

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

	for chassisIndex, chassis := range chassisList {
		// Assume a base xname for the chassis for testing purposes
		chassisXname := fmt.Sprintf("x%dc%d", cabinetID, chassisIndex)

		power, _ := chassis.Power()
		if power != nil {
			for i, psu := range power.PowerSupplies {
				if psu.Status.State == common.EnabledState {
					fru := transformPSU(&psu, chassisXname, i)
					allHardware = append(allHardware, fru)
				}
			}
		}

		systems, _ := chassis.ComputerSystems()
		for systemIndex, system := range systems {
			// Assume a base xname for the node based on its parent chassis and its own index
			// This makes a guess for slot and bmc numbers (s0b0).
			nodeXname := fmt.Sprintf("%ss0b0n%d", chassisXname, systemIndex)

			processors, _ := system.Processors()
			for i, proc := range processors {
				if proc.Status.State == common.EnabledState {
					if proc.ProcessorType == "CPU" {
						fru := transformProcessor(proc, nodeXname, i)
						allHardware = append(allHardware, fru)
					} else if proc.ProcessorType == "GPU" || proc.ProcessorType == "Accelerator" {
						fru := transformAccelerator(proc, nodeXname, i)
						allHardware = append(allHardware, fru)
					}
				}
			}

			memoryModules, _ := system.Memory()
			for i, mem := range memoryModules {
				if mem.Status.State == common.EnabledState {
					fru := transformMemory(mem, nodeXname, i)
					allHardware = append(allHardware, fru)
				}
			}

			storageControllers, _ := system.Storage()
			for storageIndex, storage := range storageControllers {
				drives, _ := storage.Drives()
				for driveIndex, drive := range drives {
					if drive.Status.State == common.EnabledState {
						fru := transformDrive(drive, nodeXname, storageIndex, driveIndex)
						allHardware = append(allHardware, fru)
					}
				}
			}

			netInterfaces, _ := system.NetworkInterfaces()
			for i, nic := range netInterfaces {
				adapter, _ := nic.NetworkAdapter()
				if adapter != nil {
					fru := transformNetworkAdapter(adapter, nodeXname, i)
					allHardware = append(allHardware, fru)
				}
			}
		}
	}

	payload := SMDHardwarePayload{
		Hardware: allHardware,
	}

	output, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal final payload: %v", err)
	}

	fmt.Fprintln(os.Stdout, string(output))
	return nil
}

func transformProcessor(proc *redfish.Processor, nodeXname string, index int) HWInventoryByLocation {
	manufacturer := strings.TrimSpace(proc.Manufacturer)
	model := strings.TrimSpace(proc.Model)
	serial := strings.TrimSpace(proc.SerialNumber)
	socket := strings.TrimSpace(proc.Socket)
	fruid := fmt.Sprintf("%s-%s-%s", manufacturer, model, serial)
	if serial == "" {
		fruid = fmt.Sprintf("%s-%s-%s", manufacturer, model, strings.ReplaceAll(socket, " ", ""))
	}

	componentType := "Processor"
	locationInfoKey := "ProcessorLocationInfo" // Derived from HWInvByLocProcessor
	locationInfo := map[string]interface{}{
		locationInfoKey: &RedfishProcessorLocationInfo{Socket: socket},
	}

	return HWInventoryByLocation{
		ID:                        fmt.Sprintf("%sp%d", nodeXname, index),
		Type:                      componentType,
		Status:                    "Populated",
		HWInventoryByLocationType: "HWInvByLocProcessor",
		LocationInfo:              locationInfo,
		PopulatedFRU: &HWInventoryByFRU{
			FRUID:                fruid,
			Type:                 componentType,
			HWInventoryByFRUType: "HWInvByFRUProcessor",
			ProcessorFRUInfo: &ProcessorFRUInfo{
				Manufacturer: manufacturer,
				Model:        model,
				TotalCores:   proc.TotalCores,
			},
		},
	}
}

func transformAccelerator(proc *redfish.Processor, nodeXname string, index int) HWInventoryByLocation {
	manufacturer := strings.TrimSpace(proc.Manufacturer)
	model := strings.TrimSpace(proc.Model)
	serial := strings.TrimSpace(proc.SerialNumber)
	socket := strings.TrimSpace(proc.Socket)
	fruid := fmt.Sprintf("%s-%s-%s", manufacturer, model, serial)
	if serial == "" {
		fruid = fmt.Sprintf("%s-%s-%s", manufacturer, model, strings.ReplaceAll(socket, " ", ""))
	}

	componentType := "NodeAccel"
	locationInfoKey := "NodeAccelLocationInfo" // Derived from HWInvByLocNodeAccel
	locationInfo := map[string]interface{}{
		// The original code used RedfishProcessorLocationInfo for accelerators, we maintain that here for the value.
		locationInfoKey: &RedfishProcessorLocationInfo{Socket: socket},
	}

	return HWInventoryByLocation{
		ID:                        fmt.Sprintf("%sa%d", nodeXname, index),
		Type:                      componentType,
		Status:                    "Populated",
		HWInventoryByLocationType: "HWInvByLocNodeAccel",
		LocationInfo:              locationInfo,
		PopulatedFRU: &HWInventoryByFRU{
			FRUID:                fruid,
			Type:                 componentType,
			HWInventoryByFRUType: "HWInvByFRUNodeAccel",
			AcceleratorFRUInfo: &AcceleratorFRUInfo{
				Manufacturer: manufacturer,
				Model:        model,
			},
		},
	}
}

func transformMemory(mem *redfish.Memory, nodeXname string, index int) HWInventoryByLocation {
	manufacturer := strings.TrimSpace(mem.Manufacturer)
	partNumber := strings.TrimSpace(mem.PartNumber)
	serial := strings.TrimSpace(mem.SerialNumber)
	fruid := fmt.Sprintf("%s-%s-%s", manufacturer, partNumber, serial)

	componentType := "Memory"
	var locationInfo map[string]interface{}

	// Per original logic, only add location info if the data is present in Redfish.
	if mem.MemoryLocation.Socket != 0 || mem.MemoryLocation.MemoryController != 0 || mem.MemoryLocation.Channel != 0 || mem.MemoryLocation.Slot != 0 {
		locationInfoKey := "MemoryLocationInfo" // Derived from HWInvByLocMemory
		locationInfo = map[string]interface{}{
			locationInfoKey: &RedfishMemoryLocationInfo{
				Socket:           mem.MemoryLocation.Socket,
				MemoryController: mem.MemoryLocation.MemoryController,
				Channel:          mem.MemoryLocation.Channel,
				Slot:             mem.MemoryLocation.Slot,
			},
		}
	}

	return HWInventoryByLocation{
		ID:                        fmt.Sprintf("%sd%d", nodeXname, index),
		Type:                      componentType,
		Status:                    "Populated",
		HWInventoryByLocationType: "HWInvByLocMemory",
		LocationInfo:              locationInfo, // Will be nil if no location data, so it won't be marshalled.
		PopulatedFRU: &HWInventoryByFRU{
			FRUID:                fruid,
			Type:                 componentType,
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
func transformDrive(drive *redfish.Drive, nodeXname string, storageIndex int, driveIndex int) HWInventoryByLocation {
	manufacturer := strings.TrimSpace(drive.Manufacturer)
	model := strings.TrimSpace(drive.Model)
	partNumber := strings.TrimSpace(drive.PartNumber)
	serial := strings.TrimSpace(drive.SerialNumber)
	fruid := fmt.Sprintf("%s-%s-%s", manufacturer, model, serial)

	componentType := "Drive"
	locationInfoKey := "DriveLocationInfo" // Derived from HWInvByLocDrive
	locationInfo := map[string]interface{}{
		locationInfoKey: &RedfishDriveLocationInfo{},
	}

	return HWInventoryByLocation{
		ID:                        fmt.Sprintf("%sg%dk%d", nodeXname, storageIndex, driveIndex),
		Type:                      componentType,
		Status:                    "Populated",
		HWInventoryByLocationType: "HWInvByLocDrive",
		LocationInfo:              locationInfo,
		PopulatedFRU: &HWInventoryByFRU{
			FRUID:                fruid,
			Type:                 componentType,
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

func transformNetworkAdapter(adapter *redfish.NetworkAdapter, nodeXname string, index int) HWInventoryByLocation {
	manufacturer := strings.TrimSpace(adapter.Manufacturer)
	model := strings.TrimSpace(adapter.Model)
	partNumber := strings.TrimSpace(adapter.PartNumber)
	serial := strings.TrimSpace(adapter.SerialNumber)
	fruid := fmt.Sprintf("%s-%s-%s", manufacturer, partNumber, serial)

	componentType := "NodeHsnNic"
	locationInfoKey := "HSNNICLocationInfo" // Derived from HWInvByLocHSNNIC
	locationInfo := map[string]interface{}{
		locationInfoKey: &RedfishNetworkAdapterLocationInfo{},
	}

	return HWInventoryByLocation{
		ID:                        fmt.Sprintf("%sh%d", nodeXname, index),
		Type:                      componentType,
		Status:                    "Populated",
		HWInventoryByLocationType: "HWInvByLocHSNNIC",
		LocationInfo:              locationInfo,
		PopulatedFRU: &HWInventoryByFRU{
			FRUID:                fruid,
			Type:                 componentType,
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

func transformPSU(psu *redfish.PowerSupply, chassisXname string, psuIndex int) HWInventoryByLocation {
	manufacturer := strings.TrimSpace(psu.Manufacturer)
	model := strings.TrimSpace(psu.Model)
	partNumber := strings.TrimSpace(psu.PartNumber)
	serial := strings.TrimSpace(psu.SerialNumber)
	fruid := fmt.Sprintf("%s-%s-%s", manufacturer, model, serial)
	componentType := "CMMRectifier" // Define type once

	// Create the dynamically-keyed location info map
	locationInfo := make(map[string]interface{})
	locationKey := fmt.Sprintf("%sLocationInfo", componentType) // Creates "CMMRectifierLocationInfo"
	locationInfo[locationKey] = &RedfishPSULocationInfo{}       // Use the existing struct for the value

	return HWInventoryByLocation{
		ID:                        fmt.Sprintf("%st%d", chassisXname, psuIndex),
		Type:                      componentType,
		Status:                    "Populated",
		HWInventoryByLocationType: "HWInvByLocCMMRectifier",
		LocationInfo:              locationInfo, // Assign the map here
		PopulatedFRU: &HWInventoryByFRU{
			FRUID:                fruid,
			Type:                 componentType,
			HWInventoryByFRUType: "HWInvByFRUCMMRectifier",
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
