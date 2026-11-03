//go:build windows

package cli

import "syscall"

// errnoName maps Winsock and Win32 errors the way libuv (uv_translate_sys_error) does for Node.js.
func errnoName(e syscall.Errno) string {
	switch e {
	case 10061, 1225: // WSAECONNREFUSED, ERROR_CONNECTION_REFUSED
		return "ECONNREFUSED"
	case 10054, 64: // WSAECONNRESET, ERROR_NETNAME_DELETED
		return "ECONNRESET"
	case 10065, 1232: // WSAEHOSTUNREACH, ERROR_HOST_UNREACHABLE
		return "EHOSTUNREACH"
	case 10051, 1231: // WSAENETUNREACH, ERROR_NETWORK_UNREACHABLE
		return "ENETUNREACH"
	case 10049: // WSAEADDRNOTAVAIL
		return "EADDRNOTAVAIL"
	case 10060, 121: // WSAETIMEDOUT, ERROR_SEM_TIMEOUT
		return "ETIMEDOUT"
	case 109, 232: // ERROR_BROKEN_PIPE, ERROR_NO_DATA
		return "EPIPE"
	case 10053, 1236: // WSAECONNABORTED, ERROR_CONNECTION_ABORTED
		return "ECONNABORTED"
	case 10050: // WSAENETDOWN
		return "ENETDOWN"
	case 10064: // WSAEHOSTDOWN
		return "EHOSTDOWN"
	case 10048: // WSAEADDRINUSE
		return "EADDRINUSE"
	case 10013: // WSAEACCES
		return "EACCES"
	}
	return ""
}
