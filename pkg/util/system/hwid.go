package system

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
)

var virtualPrefixes = []string{
	"br-", "docker", "veth", "virbr", "vmnet", "vmenet",
	"ovs", "cali", "flannel", "cni", "bond",
	"tun", "tap",
	"utun", "awdl", "llw", "anpi", "bridge", "gif", "stf",
	"vnic", "vEthernet",
}

// GenerateUserFromHardware generates a 16-char hex user identifier from
// machine ID and physical MAC addresses. Returns "" if neither is available.
func GenerateUserFromHardware() string {
	var parts []string

	if mid := getMachineID(); mid != "" {
		parts = append(parts, mid)
	}

	if macs := physicalMACs(); len(macs) > 0 {
		parts = append(parts, strings.Join(macs, ","))
	}

	if len(parts) == 0 {
		return ""
	}

	hash := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(hash[:])[:16]
}

func getMachineID() string {
	switch runtime.GOOS {
	case "linux":
		return linuxMachineID()
	case "darwin":
		return darwinMachineID()
	case "windows":
		return windowsMachineID()
	}
	return ""
}

func linuxMachineID() string {
	if data, err := os.ReadFile("/etc/machine-id"); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id
		}
	}
	if data, err := os.ReadFile("/var/lib/dbus/machine-id"); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id
		}
	}
	return ""
}

func darwinMachineID() string {
	out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "IOPlatformUUID") {
			parts := strings.SplitN(line, `" = "`, 2)
			if len(parts) == 2 {
				return strings.TrimSuffix(parts[1], `"`)
			}
		}
	}
	return ""
}

func windowsMachineID() string {
	out, err := exec.Command("reg", "query",
		`HKLM\SOFTWARE\Microsoft\Cryptography`,
		"/v", "MachineGuid",
	).Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "MachineGuid") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				return fields[len(fields)-1]
			}
		}
	}
	return ""
}

func physicalMACs() []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	var macs []string
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if len(iface.HardwareAddr) != 6 {
			continue
		}

		allZero := true
		for _, b := range iface.HardwareAddr {
			if b != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			continue
		}

		if isVirtualInterface(iface.Name) {
			continue
		}

		macs = append(macs, iface.HardwareAddr.String())
	}
	sort.Strings(macs)
	return macs
}

func isVirtualInterface(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range virtualPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}
