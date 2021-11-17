// +build mage

package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/ioutil"
	"os"
	"path"
	"runtime"
	"strings"
	"time"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

const (
	// Project base name.
	pkgName = "mercury"
	// Go package path.
	pkg = "code.ornl.gov/situ/" + pkgName
	// Directory for binaries.
	binDir = "bin"
	// Base name for the binary file (without OS/platform).
	binPath = binDir + "/" + pkgName
	// Image to use to build Linux binary in docker.
	goDockerImg = "golang:1.14"
	// URL for docker registry.
	dockerRepo = "code.ornl.gov:4567"
)

var (
	// Temporary files that will be removed in clean directive.
	tmpFiles = []string{binDir}

	// Variables injected into the build at compile time.
	ldflags = `-X main.BuildTime=$BUILD_TIME -X main.GitSHA=$GIT_SHA -X main.Version=$VERSION -X main.GoVersion=$GO_VERSION`

	// Version environment variable for creating a release.
	versEnvVar string

	// Need to capture git output.
	git = sh.OutCmd("git")

	// Allow user to override go executable by running as `GOEXE=xxx mage ..`
	// on unix-like systems.
	goexe = "go"
	goenv = map[string]string{} // map[string]string{"CGO_ENABLED": "0"}

	// Mage aliases
	Aliases = map[string]interface{}{
		"b":  Build,
		"rm": Clean,
		"pb": CompileProtobuf,
		"t":  Test,
	}

	// Used for git flow release version.
	gitFlowVersion string

	// Default target for just running `mage`.
	Default = Build
)

func init() {
	if exe := os.Getenv("GOEXE"); exe != "" {
		goexe = exe
	}
	versEnvVar = strings.ToUpper(strings.Replace(pkgName, "-", "_", -1) + "_version")
	if v := os.Getenv(versEnvVar); v != "" {
		gitFlowVersion = v
	}
}

// Compile protocol buffers.
func CompileProtobuf() error {
	mg.SerialDeps(Deps)
	protoc := sh.RunCmd("protoc", "-I", "./api")
	err := protoc("--go_out=plugins=grpc,paths=source_relative:api", "v1/api.proto")
	if err != nil {
		return err
	}
	err = protoc("--grpc-gateway_out=logtostderr=true,paths=source_relative:api", "v1/api.proto")
	if err != nil {
		return err
	}
	return nil
}

// Compile binary.
// Compile binary for the local platform/operating system only.
func Build() error {
	mg.SerialDeps(CompileProtobuf, Vet)
	opersys := runtime.GOOS
	arch := runtime.GOARCH
	out := binPath + "-" + opersys + "-" + arch
	e := env(opersys, arch)
	return sh.RunWith(e, goexe, "build", "-o", out, "-ldflags", ldflags)
}

// Compile linux binary.
// Compile linux binary for 386 and amd64 platforms.
func BuildLinux() error {
	opersys := "linux"
	arch := "amd64"
	out := binPath + "-" + opersys + "-" + arch
	e := env(opersys, arch)
	buildScript := "apt update && apt install -y --no-install-recommends libpcap0.8 libpcap0.8-dev protobuf-compiler libprotobuf-dev && go build -o " + out + " -ldflags \"" + ldflags + "\""
	err := sh.RunWith(e, "docker", "run", "--rm", "-it", "-v", "$PWD:/work", "-w", "/work", goDockerImg, "/bin/sh", "-c", buildScript)
	return err
}

// Run go vet.
// Run the static source code analysis command, vet.
func Vet() error {
	return sh.RunWith(goenv, goexe, "vet", "./...")
}

// Run unit tests.
// Run all golang unit tests.
func Test() error {
	fmt.Println("Running unit tests")
	return sh.RunWith(goenv, goexe, "test", "-short", "./...")
}

// Download dependencies.
// Download dependencies that are listed in `tools/tools.go`.
func Deps() error {
	fmt.Println("Downloading build tools")
	fset := token.NewFileSet() // positions are relative to fset
	toolsFile, err := os.Open(path.Join("tools", "tools.go"))
	if err != nil {
		return err
	}
	toolsSrc, err := ioutil.ReadAll(toolsFile)
	if err != nil {
		return err
	}
	f, err := parser.ParseFile(fset, "", toolsSrc, parser.ImportsOnly)
	if err != nil {
		return err
	}
	for _, s := range f.Imports {
		pkg := strings.Replace(s.Path.Value, `"`, ``, -1)
		err = sh.RunWith(goenv, goexe, "install", pkg)
		if err != nil {
			return err
		}
	}
	return nil
}

// Remove build files.
// Remove all temporary and build files.
func Clean() error {
	fmt.Printf("Removing temporary files: %s\n", strings.Join(tmpFiles, ", "))
	for _, f := range tmpFiles {
		sh.Rm(f)
	}
	return nil
}

// -----------------------------------------------------------
// Private functions
// -----------------------------------------------------------

// env will set up the environment variables needed to build the software.
func env(opersys, arch string) map[string]string {
	branch, _ := git("rev-parse", "--abbrev-ref", "HEAD")
	version, _ := git("describe", branch, "--tags", "--abbrev=0")
	hash, _ := git("rev-parse", "--short", "HEAD")
	goVersion, _ := sh.Output(goexe, "version")
	pwd, ok := os.LookupEnv("PWD")
	if !ok {
		fmt.Println("environment variable PWD not set.")
	}
	gobin := pwd + "/bin"
	path, ok := os.LookupEnv("PATH")
	if !ok {
		fmt.Println("environment variable PATH not set.")
	}
	env := map[string]string{
		"GOBIN":      gobin,
		"PATH":       gobin + ":" + path,
		"VERSION":    version,
		"GIT_SHA":    hash,
		"BUILD_TIME": time.Now().Format("2006-01-02T15:04:05Z0700"),
		"GO_VERSION": strings.Split(goVersion, " ")[2],
		"GOOS":       opersys,
		"GOARCH":     arch,
	}
	for k, v := range goenv {
		env[k] = v
	}
	return env
}
