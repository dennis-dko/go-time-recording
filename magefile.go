//go:build mage
// +build mage

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

const AppName = "go-time-recording"

// Default target to run when no target is specified.
// Standard: `mage` or `mage build`
var Default = Build

// Build compiles the Go application with a default environment (e.g., "dev").
// Standard: `mage build`
func Build() error {
	return BuildEnv("dev")
}

// Build compiles the Go application.
// Standard: `mage buildenv local / dev / staging / prod`
func BuildEnv(env string) error {
	mg.Deps(Clean, Tidy) // Ensure go.mod and go.sum are tidy before building
	fmt.Println("Building application for " + env)
	// Ensure the output directory exists
	if err := os.MkdirAll("bin", os.ModePerm); err != nil {
		return fmt.Errorf("failed to create bin directory: %w", err)
	}

	fmt.Println("Copying configs directory...")
	if runtime.GOOS == "windows" {
		if err := sh.RunV("xcopy", "/E", "/I", "cmd\\configs", "bin\\configs"); err != nil {
			return fmt.Errorf("failed to copy configs directory (Windows): %w", err)
		}
	} else {
		if err := sh.RunV("cp", "-r", "cmd/configs", "bin/"); err != nil {
			return fmt.Errorf("failed to copy configs directory (Unix/Linux/macOS): %w", err)
		}
	}

	// Determine the executable name based on OS
	outputName := AppName
	if runtime.GOOS == "windows" {
		outputName += ".exe"
	}

	// Go build command
	cmd := exec.Command("go", "build", "-o", "./bin/"+outputName, "./cmd/main.go")

	// Set app environment
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "APP_ENV="+env)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil { // Run the build command and check for errors
		return fmt.Errorf("go build failed: %w", err)
	}

	fmt.Println(cmd.Env)

	fmt.Println("Build and configs copy completed successfully.")
	return nil
}

// runCompiledApp is a helper function to run the already compiled application.
// It does NOT set APP_ENV at runtime, as it's already embedded.
func runCompiledApp() error {
	fmt.Println("Running compiled application...")

	outputName := AppName
	if runtime.GOOS == "windows" {
		outputName += ".exe"
	}

	cmd := exec.Command("./bin/" + outputName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunLocal builds and executes the application locally with APP_ENV=local.
// Standard: `mage runLocal`
func RunLocal() error {
	mg.Deps(Clean, mg.F(BuildEnv, "local"))
	return runCompiledApp()
}

// RunDev builds and executes the application for the development environment with APP_ENV=dev.
// Standard: `mage runDev`
func RunDev() error {
	mg.Deps(Clean, mg.F(BuildEnv, "dev"))
	return runCompiledApp()
}

// RunStaging builds and executes the application for the staging environment with APP_ENV=staging.
// Standard: `mage runStaging`
func RunStaging() error {
	mg.Deps(Clean, mg.F(BuildEnv, "staging"))
	return runCompiledApp()
}

// RunProd builds and executes the application for the production environment with APP_ENV=prod.
// Standard: `mage runProd`
func RunProd() error {
	mg.Deps(Clean, mg.F(BuildEnv, "prod"))
	return runCompiledApp()
}

// Test runs all unit and integration tests.
// Standard: `mage test`
func Test() error {
	mg.Deps(Tidy) // Ensure go.mod and go.sum are tidy before testing
	fmt.Println("Running tests...")
	// Run all tests, including those in subdirectories
	return sh.RunV("go", "test", "./...")
}

// Tidy ensures go.mod and go.sum are tidy.
// Standard: `mage tidy`
func Tidy() error {
	fmt.Println("Running go mod tidy...")
	return sh.RunV("go", "mod", "tidy")
}

// Clean removes build artifacts.
// Standard: `mage clean`
func Clean() error {
	fmt.Println("Cleaning build artifacts...")
	return os.RemoveAll("bin")
}

// Deploy would contain commands to build Docker images, push to a registry,
// or deploy to a specific environment (e.g., Kubernetes, cloud provider).
// Standard: `mage deploy`
func Deploy() error {
	mg.Deps(Clean, Build) // Ensure the application is built
	fmt.Println("Deploying application...")

	// Build a Docker image
	fmt.Println("Building Docker image...")
	if err := sh.RunV("docker", "build", "-t", AppName+":latest", "."); err != nil {
		return fmt.Errorf("failed to build docker image: %w", err)
	}

	return nil
}

// All runs all common targets (clean, build, test).
// Standard: `mage all`
func All() {
	mg.Deps(Clean, Build, Test)
}
