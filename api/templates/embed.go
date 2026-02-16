// Package templates provides embedded template files for node installation
package templates

import _ "embed"

//go:generate cp ../../../docker/docker-compose.yml docker-compose.yml

// InitNodeScript is the node installation bash script
//
//go:embed init-node.sh
var InitNodeScript []byte

// DockerCompose is the docker-compose.yml for kaitu-slave deployment
// Synced from docker/docker-compose.yml via go:generate
//
//go:embed docker-compose.yml
var DockerCompose []byte
