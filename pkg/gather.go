package magellan

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings" // Added for string manipulation
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

	// Create a custom HTTP client with longer timeouts and proxy disabled.
	tr := &http.Transport{
		Proxy:           nil, // Do not use the system's http_proxy settings.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	httpClient := &http.Client{
		Transport: tr,
		Timeout:   30 * time.Second,
	}

	for _, host := range hosts {
		// 1. Get credentials and connect to the BMC
		creds, err := bmc.GetBMCCredentials(params.SecretStore, host)
		if err != nil {
			return fmt.Errorf("failed to get credentials for %s: %v", host, err)
		}

		config := gofish.ClientConfig{
			Endpoint:   "https://" + host,
			Username:   creds.Username,
			Password:   creds.Password,
			Insecure:   true,
			HTTPClient: httpClient, // Pass the custom client to gofish
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
			processors, err := system.Processors()
			if err == nil {
				for _, proc := range processors {
					if proc.Status.State == common.EnabledState {
						fru := transformProcessor(proc, system.ID)
						allHardware = append(allHardware, fru)
					}
				}
			}

			memoryModules, err := system.Memory()
			if err == nil {
				for _, mem := range memoryModules {
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
	// Refinement: Trim whitespace from all incoming strings
	manufacturer := strings.TrimSpace(proc.Manufacturer)
	model := strings.TrimSpace(proc.Model)
	serial := strings.TrimSpace(proc.SerialNumber)
	socket := strings.TrimSpace(proc.Socket)
	// Refinement: Remove spaces from socket identifier for cleaner xnames
	socketID := strings.ReplaceAll(socket, " ", "")

	fruid := fmt.Sprintf("%s-%s-%s", manufacturer, model, serial)
	if serial == "" {
		fruid = fmt.Sprintf("%s-%s-%s", manufacturer, model, socketID)
	}

	return HWInventoryByLocation{
		ID:                        fmt.Sprintf("%sp%s", nodeXname, socketID),
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

// transformMemory converts a gofish Memory object to an SMD-compatible struct
func transformMemory(mem *redfish.Memory, nodeXname string) HWInventoryByLocation {
	// Refinement: Trim whitespace from all incoming strings
	manufacturer := strings.TrimSpace(mem.Manufacturer)
	partNumber := strings.TrimSpace(mem.PartNumber)
	serial := strings.TrimSpace(mem.SerialNumber)
	deviceLocator := strings.TrimSpace(mem.DeviceLocator)
	// Refinement: Remove spaces from locator for cleaner xnames
	locatorID := strings.ReplaceAll(deviceLocator, " ", "")

	fruid := fmt.Sprintf("%s-%s-%s", manufacturer, partNumber, serial)

	return HWInventoryByLocation{
		ID:                        fmt.Sprintf("%sd%s", nodeXname, locatorID),
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
