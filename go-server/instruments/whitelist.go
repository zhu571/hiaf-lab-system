package instruments

import (
	_ "embed"
	"fmt"

	"gopkg.in/yaml.v3"
)

//go:embed whitelist_embedded.yaml
var whitelistYAML []byte

type whitelistFile struct {
	Version     string                   `yaml:"version"`
	Policy      map[string]any           `yaml:"policy"`
	Instruments map[string]instrumentDef `yaml:",inline"`
}

type instrumentDef struct {
	Name       string       `yaml:"name"`
	Type       string       `yaml:"type"`
	Interface  string       `yaml:"interface"`
	Optional   bool         `yaml:"optional"`
	IP         string       `yaml:"ip"`
	Port       int          `yaml:"port"`
	Terminator string       `yaml:"terminator"`
	Commands   []CommandDef `yaml:"commands"`
}

var whitelist = loadWhitelist()
var whitelistVersion string

func loadWhitelist() map[string]instrumentDef {
	var file whitelistFile
	if err := yaml.Unmarshal(whitelistYAML, &file); err != nil {
		panic(fmt.Errorf("parse instrument whitelist: %w", err))
	}
	if file.Version == "" || len(file.Policy) == 0 || len(file.Instruments) == 0 {
		panic("instrument whitelist requires version, policy, and instruments")
	}
	whitelistVersion = file.Version
	for id, instrument := range file.Instruments {
		if id == "" || instrument.Name == "" || instrument.Type == "" || instrument.Interface == "" || (!instrument.Optional && (instrument.IP == "" || instrument.Port <= 0)) || instrument.Terminator == "" || len(instrument.Commands) == 0 {
			panic(fmt.Sprintf("instrument whitelist entry %q has incomplete fields", id))
		}
		seen := make(map[string]bool, len(instrument.Commands))
		for _, command := range instrument.Commands {
			if command.Name == "" || command.Description == "" || (command.Risk != "green" && command.Risk != "yellow" && command.Risk != "red") || (command.SCPI == "" && command.Build == "" && command.Risk != "red") || command.TimeoutMS < 0 {
				panic(fmt.Sprintf("instrument %q command %q has incomplete fields", id, command.Name))
			}
			if seen[command.Name] {
				panic(fmt.Sprintf("instrument %q has duplicate command %q", id, command.Name))
			}
			seen[command.Name] = true
		}
	}
	return file.Instruments
}

func InstrumentName(id string) string { return whitelist[id].Name }

// GetCommand returns a copy of a named command definition.
func GetCommand(instrumentID, commandName string) (*CommandDef, error) {
	for _, command := range whitelist[instrumentID].Commands {
		if command.Name == commandName {
			return &command, nil
		}
	}
	return nil, fmt.Errorf("command %q is not allowed for instrument %q", commandName, instrumentID)
}

// ListCommands returns a copy of an instrument's command definitions.
func ListCommands(instrumentID string) []CommandDef {
	commands := whitelist[instrumentID].Commands
	return append([]CommandDef(nil), commands...)
}

// IsCommandAllowed reports whether a command exists at the requested risk level.
func IsCommandAllowed(instrumentID, commandName, riskLevel string) bool {
	command, err := GetCommand(instrumentID, commandName)
	return err == nil && command.Risk == riskLevel
}
