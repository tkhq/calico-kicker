package calico

import (
	"log/slog"
	"net"
	"slices"

	"github.com/jsimonetti/rtnetlink/v2"
)

const MainRouteTable = 254

var DefaultInterfaceNames = []string{
	"wireguard.cali",
	"wg-v6.cali",
}

// AllIPsExist checks to see whether each named interface has a corresponding address on it.
func AllIPsExist(conn *rtnetlink.Conn, interfaceNames []string) bool {
	interfaceIndices := InterfaceIndices(conn, interfaceNames)
	if len(interfaceIndices) != len(interfaceNames) {
		slog.Warn("insufficient interfaces found",
			slog.Int("count", len(interfaceIndices)),
			slog.Int("expected", len(interfaceNames)),
		)

		return false
	}

	selfIPs := SelfIPs(conn, interfaceIndices)
	if len(selfIPs) != len(interfaceNames) {
		slog.Warn("unexpected number of IP addresses found",
			slog.Int("count", len(selfIPs)),
			slog.Int("expected", len(interfaceNames)),
		)

		return false
	}

	return true
}

func InterfaceIndices(conn *rtnetlink.Conn, interfaceNames []string) (indices []uint32) {
	interfaces, err := conn.Link.List()
	if err != nil {
		slog.Error("failed to get list of interfaces", slog.String("error", err.Error()))

		return nil
	}

	for _, i := range interfaces {
		if i.Attributes == nil {
			slog.Debug("ignoring interface with no attributes", slog.Uint64("interface", uint64(i.Index)))

			continue
		}

		if slices.Contains(interfaceNames, i.Attributes.Name) {
			indices = append(indices, i.Index)
		}
	}

	return indices
}

func SelfIPs(conn *rtnetlink.Conn, interfaceIndices []uint32) (list []net.IP) {
	addresses, err := conn.Address.List()
	if err != nil {
		slog.Error("failed to list IP addresses", slog.String("error", err.Error()))

		return nil
	}

	for _, addr := range addresses {
		if addr.Attributes == nil {
			slog.Debug("ignoring address with no attributes", slog.Uint64("address", uint64(addr.Index)))

			continue
		}
		log := slog.With(
			slog.String("address", addr.Attributes.Address.String()),
		)

		if slices.Contains(interfaceIndices, addr.Index) {
			log.Debug("found address for interface")

			list = append(list, addr.Attributes.Address)

			continue
		}

		log.Debug("ignoring address of non-specified interface")

		continue
	}

	return list
}
