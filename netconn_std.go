package websocket

import "net"

func (nc *netConn) RemoteAddr() net.Addr {
	if unc, ok := nc.c.conn().(net.Conn); ok {
		return unc.RemoteAddr()
	}
	return websocketAddr{}
}

func (nc *netConn) LocalAddr() net.Addr {
	if unc, ok := nc.c.conn().(net.Conn); ok {
		return unc.LocalAddr()
	}
	return websocketAddr{}
}
