package main

import (
	"fmt"
	"os"

	"github.com/matteomeneghetti/windlock/container"
	"github.com/matteomeneghetti/windlock/doctor"
	"github.com/matteomeneghetti/windlock/invoke"
	"github.com/matteomeneghetti/windlock/tools"
)

// cmdDoctor prints one line per check, and exits 1 if any failed.
func cmdDoctor(args []string, s streams) error {
	if len(args) != 0 {
		return errUsage
	}
	if container.Inside() {
		return fmt.Errorf("doctor runs on the host")
	}
	project, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := tools.Root()
	if err != nil {
		return err
	}
	results := doctor.Run(doctor.Default(project, root))
	for _, r := range results {
		line := fmt.Sprintf("%-5s %-9s %s", r.Status, r.Name, r.Detail)
		if r.Fix != "" {
			line += "; " + r.Fix
		}
		fmt.Fprintln(s.out, line)
	}
	if doctor.Failed(results) {
		return &invoke.ExitError{Code: 1}
	}
	return nil
}
