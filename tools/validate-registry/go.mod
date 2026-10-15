module validate-registry

go 1.26.0

require github.com/router-for-me/CLIProxyAPI/v8 v8.0.0

require (
	github.com/sirupsen/logrus v1.9.3 // indirect
	golang.org/x/sys v0.47.0 // indirect
)

replace github.com/router-for-me/CLIProxyAPI/v8 => ../../../CLIProxyAPI
