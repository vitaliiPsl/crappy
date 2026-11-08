package memory

import "fmt"

const (
	ScopePersonal = "personal"
	ScopeWorkdir  = "workdir"
)

func resolveScope(scope, directory string) (string, error) {
	switch scope {
	case ScopePersonal:
		return scope, nil
	case ScopeWorkdir:
		return workdirScope(directory), nil
	default:
		return "", fmt.Errorf("unknown memory namespace %q: use %s or %s", scope, ScopePersonal, ScopeWorkdir)
	}
}

func workdirScope(directory string) string {
	return ScopeWorkdir + ":" + directory
}
