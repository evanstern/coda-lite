package cli

import "github.com/evanstern/coda-lite/internal/paths"

func agentsDir() (string, error)           { return paths.AgentsDir() }
func agentDir(name string) (string, error) { return paths.AgentDir(name) }
func agentExists(name string) (bool, string, error) {
	return paths.AgentExists(name)
}
func mustAgentDir(name string) (string, error) { return paths.MustAgentDir(name) }
func validateName(name string) error           { return paths.ValidateName(name) }
