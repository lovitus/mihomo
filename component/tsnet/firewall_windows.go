//go:build windows

package tsnet

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/metacubex/mihomo/log"
)

const wgFirewallRuleName = "Mihomo-Tailscale-WireGuard"

func platformAddFirewallRule() {
	exe, err := os.Executable()
	if err != nil {
		log.Warnln("[Tailscale] windows: failed to get executable path for firewall rule: %v", err)
		return
	}
	// Delete any existing rule first to avoid duplicates
	_ = exec.Command("netsh", "advfirewall", "firewall", "delete", "rule",
		"name="+wgFirewallRuleName).Run()
	// Intentionally allow inbound traffic to the whole mihomo.exe process.
	// This keeps tsnet/WireGuard reachable on Windows and also avoids LAN-facing
	// mihomo listeners such as mixed/controller ports being blocked by the local firewall.
	err = exec.Command("netsh", "advfirewall", "firewall", "add", "rule",
		fmt.Sprintf("name=%s", wgFirewallRuleName),
		"dir=in",
		"action=allow",
		fmt.Sprintf("program=%s", exe)).Run()
	if err != nil {
		log.Warnln("[Tailscale] windows: failed to add firewall rule (requires Administrator): %v", err)
		log.Warnln("[Tailscale] windows: this is a process-wide inbound allow rule; to add it manually, run as admin: netsh advfirewall firewall add rule name=\"%s\" dir=in action=allow program=\"%s\"",
			wgFirewallRuleName, exe)
	} else {
		log.Infoln("[Tailscale] windows: added process-wide inbound firewall rule for %s", exe)
	}
}

func platformRemoveFirewallRule() {
	_ = exec.Command("netsh", "advfirewall", "firewall", "delete", "rule",
		"name="+wgFirewallRuleName).Run()
	log.Debugln("[Tailscale] windows: removed inbound firewall rule")
}
