// Package version exposes the build-time version string for the debeasy binary.
//
// At build time the release workflow injects the git tag via:
//
//	-ldflags "-X github.com/pfortini/debeasy/internal/version.Version=vX.Y.Z"
//
// Local / unreleased builds leave the default "dev" value, which the update
// checker interprets as "don't bug the developer about new releases".
package version

var Version = "dev"
