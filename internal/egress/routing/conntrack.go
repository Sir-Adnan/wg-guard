package routing

import (
	"encoding/binary"
	"errors"
)

// Active-connection counting for the least_conn strategy. The kernel's
// ctnetlink dump filters by (ct mark & mask) == value, so each count costs
// one dump of only that outbound's flows; messages are counted, not parsed.

const (
	nfnlSubsysCTNetlink = 1
	ipctnlMsgCTNew      = 0
	ipctnlMsgCTGet      = 1
	ipctnlMsgCTDelete   = 2
	ctaMark             = 8
	ctaMarkMask         = 21
	nlmFRequest         = 0x1
	nlmFAck             = 0x4
	nlmFDump            = 0x300
	nlmsgError          = 2
	nlmsgDone           = 3
	nlmsgHdrLen         = 16
	// maxCountedFlows bounds one dump.
	maxCountedFlows = 1_000_000
)

// ErrCountUnavailable reports that connection counts cannot be read (no
// conntrack netlink access); least_conn then balances by weight only.
var ErrCountUnavailable = errors.New("routing: connection counts unavailable")

// conntrackDumpRequest builds the filtered dump request for one mark.
func conntrackDumpRequest(seq uint32, mark uint32) []byte {
	return conntrackMarkRequest(ipctnlMsgCTGet, nlmFRequest|nlmFDump, seq, mark)
}

// conntrackFlushRequest builds a delete request without a tuple: the kernel
// then flushes every entry whose (mark & mask) equals mark (ctnetlink mark
// filter, Linux 4.6+). nfgen version 0 keeps the family filter off, as the
// conntrack tool does.
func conntrackFlushRequest(seq uint32, mark uint32) []byte {
	return conntrackMarkRequest(ipctnlMsgCTDelete, nlmFRequest|nlmFAck, seq, mark)
}

func conntrackMarkRequest(msgType, flags uint16, seq uint32, mark uint32) []byte {
	attr := func(typ uint16, v uint32) []byte {
		b := make([]byte, 8)
		binary.NativeEndian.PutUint16(b[0:2], 8)
		binary.NativeEndian.PutUint16(b[2:4], typ)
		binary.BigEndian.PutUint32(b[4:8], v)
		return b
	}
	body := []byte{2 /* AF_INET */, 0 /* NFNETLINK_V0 */, 0, 0}
	body = append(body, attr(ctaMark, mark)...)
	body = append(body, attr(ctaMarkMask, MarkMask)...)
	msg := make([]byte, nlmsgHdrLen, nlmsgHdrLen+len(body))
	binary.NativeEndian.PutUint32(msg[0:4], uint32(nlmsgHdrLen+len(body)))
	binary.NativeEndian.PutUint16(msg[4:6], nfnlSubsysCTNetlink<<8|msgType)
	binary.NativeEndian.PutUint16(msg[6:8], flags)
	binary.NativeEndian.PutUint32(msg[8:12], seq)
	return append(msg, body...)
}

// countDumpMessages counts conntrack entries in one receive buffer. done
// reports the end of the dump; a netlink error message is returned as err.
func countDumpMessages(buf []byte, seq uint32) (n int64, done bool, err error) {
	for len(buf) >= nlmsgHdrLen {
		size := binary.NativeEndian.Uint32(buf[0:4])
		typ := binary.NativeEndian.Uint16(buf[4:6])
		if size < nlmsgHdrLen || int(size) > len(buf) {
			return n, false, ErrCountUnavailable
		}
		if binary.NativeEndian.Uint32(buf[8:12]) == seq {
			switch typ {
			case nlmsgDone:
				return n, true, nil
			case nlmsgError:
				if size >= nlmsgHdrLen+4 && int32(binary.NativeEndian.Uint32(buf[16:20])) != 0 {
					return n, true, ErrCountUnavailable
				}
				return n, true, nil
			case nfnlSubsysCTNetlink<<8 | ipctnlMsgCTNew:
				n++
			}
		}
		aligned := (size + 3) &^ 3
		if int(aligned) > len(buf) {
			break
		}
		buf = buf[aligned:]
	}
	return n, false, nil
}

// ackCode reads the netlink acknowledgement for seq: the error code of the
// first NLMSG_ERROR message (0 is success).
func ackCode(buf []byte, seq uint32) (int32, bool) {
	for len(buf) >= nlmsgHdrLen {
		size := binary.NativeEndian.Uint32(buf[0:4])
		if size < nlmsgHdrLen || int(size) > len(buf) {
			return 0, false
		}
		if binary.NativeEndian.Uint16(buf[4:6]) == nlmsgError && binary.NativeEndian.Uint32(buf[8:12]) == seq && size >= nlmsgHdrLen+4 {
			return int32(binary.NativeEndian.Uint32(buf[16:20])), true
		}
		aligned := (size + 3) &^ 3
		if int(aligned) > len(buf) {
			break
		}
		buf = buf[aligned:]
	}
	return 0, false
}
