package cli

import (
	"errors"
	"fmt"
	"text/tabwriter"

	"github.com/Enriquefft/yap/internal/platform"
	"github.com/spf13/cobra"
)

// newDevicesCmd builds the `yap devices` command. It enumerates audio
// input devices via the platform's DeviceLister and prints them as a
// table. The default device is marked with an asterisk so users know
// which one general.audio_device="" resolves to.
func newDevicesCmd(p platform.Platform) *cobra.Command {
	return &cobra.Command{
		Use:   "devices",
		Short: "list available audio input devices",
		Long: `devices enumerates the audio input devices yap can record from.

The DEFAULT column marks the device the OS reports as the system
default; an empty general.audio_device in your config resolves to
that one. NAME is what you set general.audio_device to when you
want to pin a specific device.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDevices(cmd, p)
		},
	}
}

func runDevices(cmd *cobra.Command, p platform.Platform) error {
	if p.DeviceLister == nil {
		return errors.New("devices: list: platform does not support enumeration")
	}
	// Resolve the backend first so users who only care about the
	// "which backend am I on?" question see it even when enumeration
	// fails later for an unrelated reason. A failure here is still
	// fatal because there is no meaningful fallback — we cannot list
	// devices without a backend in the first place.
	backend, err := p.DeviceLister.Backend()
	if err != nil {
		return fmt.Errorf("devices: backend: %w", err)
	}
	devices, err := p.DeviceLister.ListDevices()
	if err != nil {
		return fmt.Errorf("devices: list: %w", err)
	}
	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "BACKEND: %s\n", backend); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "DEFAULT\tNAME\tDESCRIPTION")
	for _, d := range devices {
		marker := " "
		if d.IsDefault {
			marker = "*"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", marker, d.Name, d.Description)
	}
	return tw.Flush()
}
