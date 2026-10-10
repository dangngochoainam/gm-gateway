package diregistry

import (
	"github.com/dangngochoainam/gm-gateway/configs"
	"go.uber.org/dig"
)

const (
	ConfigDIName string = "Config"
)

var (
	container *dig.Container
)

func BuildContainer() {
	ctn := dig.New()

	// CONFIG
	ctn.Provide(configs.LoadConfig)

	// No database here on purpose: the gateway is stateless so it can scale
	// horizontally and restart freely. State belongs to the domain services.

	container = ctn
}

func GetDependency[T any]() T {
	var out T

	err := container.Invoke(func(dep T) {
		out = dep
	})
	if err != nil {
		panic(err)
	}

	return out
}
