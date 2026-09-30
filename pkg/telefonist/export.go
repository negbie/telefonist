package telefonist

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const flowSeparator = "\n----------------------------------------\n\n"

var flowSkipKeys = map[string]bool{
	"event": true, "time": true, "type": true,
	"RawJSON": true, "run_id": true, "token": true,
}

func filterFlowEvents(events []map[string]interface{}) string {
	var sb strings.Builder
	first := true
	for _, e := range events {
		etype, _ := e["type"].(string)
		token, _ := e["token"].(string)

		if etype == "RAW" {
			if !first {
				sb.WriteString(flowSeparator)
			}
			first = false
			param, _ := e["param"].(string)
			sb.WriteString(param)
			sb.WriteByte('\n')
			continue
		}

		if etype == "SIP" || etype == "LOG" {
			continue
		}
		if etype == "" && (token == "SIP" || token == "LOG") {
			continue
		}

		if !first {
			sb.WriteString(flowSeparator)
		}
		first = false

		timeStr, _ := e["time"].(string)
		if etype == "" {
			etype = token
		}

		sb.WriteString(fmt.Sprintf("[%s] %s\n", timeStr, strings.ToUpper(etype)))

		keys := make([]string, 0, len(e))
		for k := range e {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			if flowSkipKeys[k] {
				continue
			}
			sval := fmt.Sprintf("%v", e[k])
			if sval == "" {
				continue
			}
			sb.WriteString(strings.ToUpper(k[:1]))
			sb.WriteString(k[1:])
			sb.WriteString(": ")
			sb.WriteString(sval)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func filterTypedEvents(events []map[string]interface{}, eventType string) string {
	var sb strings.Builder
	for _, e := range events {
		t, _ := e["type"].(string)
		if t == eventType {
			param, _ := e["param"].(string)
			sb.WriteString(param)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func parseEvents(raw string) []map[string]interface{} {
	var events []map[string]interface{}
	parts := strings.Split(raw, "\n\n")
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			continue
		}
		var e map[string]interface{}
		if err := json.Unmarshal([]byte(p), &e); err == nil {
			events = append(events, e)
		} else {
			events = append(events, map[string]interface{}{
				"type":  "RAW",
				"param": p,
			})
		}
	}
	return events
}

func serveTextDownload(w http.ResponseWriter, filename, content string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	if _, err := w.Write([]byte(content)); err != nil {
		log.Printf("failed to write text download response: %v", err)
	}
}

func generatePcap(events []map[string]interface{}) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{
		0xd4, 0xc3, 0xb2, 0xa1, // magic
		0x02, 0x00, 0x04, 0x00, // version 2.4
		0x00, 0x00, 0x00, 0x00, // thiszone
		0x00, 0x00, 0x00, 0x00, // sigfigs
		0xff, 0xff, 0x00, 0x00, // snaplen
		0x01, 0x00, 0x00, 0x00, // Ethernet
	})

	lastSec := uint32(time.Now().Unix())
	lastUsec := uint32(0)

	for _, e := range events {
		t, _ := e["type"].(string)
		param, _ := e["param"].(string)

		if t != "SIP" && !(t == "LOG" && (strings.Contains(param, "|TX ") || strings.Contains(param, "|RX "))) {
			continue
		}

		lines := strings.Split(param, "\n")
		if len(lines) < 2 {
			continue
		}

		sec, usec := lastSec, lastUsec
		if idx := strings.IndexByte(lines[0], '|'); idx > 0 {
			if pt, err := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(lines[0][:idx]), time.Local); err == nil {
				sec = uint32(pt.Unix())
				usec = 0
			}
		}
		if sec == lastSec && usec <= lastUsec {
			usec = lastUsec + 1
		}
		lastSec, lastUsec = sec, usec

		addrLine := lines[0]
		payloadIdx := 1
		if len(lines) > 1 && strings.Contains(lines[1], " -> ") {
			addrLine = lines[1]
			payloadIdx = 2
		}
		srcIP, srcPort, dstIP, dstPort := parseSipTraceLine(addrLine)
		if srcIP == nil || dstIP == nil {
			continue
		}

		payloadStr := strings.ReplaceAll(strings.Join(lines[payloadIdx:], "\n"), "\r\n", "\n")
		payload := []byte(strings.ReplaceAll(strings.TrimSpace(payloadStr), "\n", "\r\n") + "\r\n\r\n")

		if len(payload) > 65507 {
			continue
		}
		ipv6 := srcIP.To4() == nil || dstIP.To4() == nil
		if (srcIP.To4() == nil) != (dstIP.To4() == nil) {
			continue
		}
		totalLen := uint32(len(payload) + 42)
		if ipv6 {
			totalLen += 20
		}
		if stamp, ok := e["timestamp"].(float64); ok {
			sec = uint32(int64(stamp) / 1000)
			usec = uint32(int64(stamp)%1000) * 1000
		}

		buf.Write([]byte{
			byte(sec), byte(sec >> 8), byte(sec >> 16), byte(sec >> 24),
			byte(usec), byte(usec >> 8), byte(usec >> 16), byte(usec >> 24),
			byte(totalLen), byte(totalLen >> 8), byte(totalLen >> 16), byte(totalLen >> 24),
			byte(totalLen), byte(totalLen >> 8), byte(totalLen >> 16), byte(totalLen >> 24),
		})

		ether := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x08, 0x00}
		if ipv6 {
			ether[12], ether[13] = 0x86, 0xdd
		}
		buf.Write(ether)

		if !ipv6 {
			ipTotalLen := uint16(28 + len(payload))
			ipHeader := []byte{
				0x45, 0x00, // Version/IHL, TOS
				byte(ipTotalLen >> 8), byte(ipTotalLen), // Total Length
				0x00, 0x01, // ID
				0x40, 0x00, // Flags (DF), Fragment
				0x40, 17, // TTL, Protocol (UDP)
				0x00, 0x00, // Checksum (filled below)
				srcIP.To4()[0], srcIP.To4()[1], srcIP.To4()[2], srcIP.To4()[3],
				dstIP.To4()[0], dstIP.To4()[1], dstIP.To4()[2], dstIP.To4()[3],
			}
			cs := ipChecksum(ipHeader)
			ipHeader[10], ipHeader[11] = byte(cs>>8), byte(cs)
			buf.Write(ipHeader)
		} else {
			header := make([]byte, 40)
			header[0], header[6], header[7] = 0x60, 17, 64
			binary.BigEndian.PutUint16(header[4:6], uint16(8+len(payload)))
			copy(header[8:24], srcIP.To16())
			copy(header[24:40], dstIP.To16())
			buf.Write(header)
		}

		udpLen := uint16(8 + len(payload))
		udp := []byte{
			byte(srcPort >> 8), byte(srcPort),
			byte(dstPort >> 8), byte(dstPort),
			byte(udpLen >> 8), byte(udpLen),
			0x00, 0x00,
		}
		if ipv6 {
			pseudo := make([]byte, 40)
			copy(pseudo, srcIP.To16())
			copy(pseudo[16:], dstIP.To16())
			binary.BigEndian.PutUint32(pseudo[32:36], uint32(udpLen))
			pseudo[39] = 17
			check := append(append(pseudo, udp...), payload...)
			if len(check)%2 != 0 {
				check = append(check, 0)
			}
			cs := ipChecksum(check)
			if cs == 0 {
				cs = 0xffff
			}
			binary.BigEndian.PutUint16(udp[6:], cs)
		}
		buf.Write(udp)

		buf.Write(payload)
	}

	return buf.Bytes()
}

func ipChecksum(header []byte) uint16 {
	var sum uint32
	for i := 0; i < len(header); i += 2 {
		sum += uint32(header[i])<<8 | uint32(header[i+1])
	}
	for sum > 0xffff {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

func parseSipTraceLine(line string) (net.IP, uint16, net.IP, uint16) {
	if idx := strings.IndexByte(line, '|'); idx >= 0 {
		line = line[idx+1:]
	}

	parts := strings.Fields(line)
	for i, p := range parts {
		if p != "->" || i == 0 || i >= len(parts)-1 {
			continue
		}
		sHost, sPortStr, errS := net.SplitHostPort(parts[i-1])
		dHost, dPortStr, errD := net.SplitHostPort(parts[i+1])
		if errS != nil || errD != nil {
			break
		}
		sIP := net.ParseIP(sHost)
		dIP := net.ParseIP(dHost)
		if sIP == nil || dIP == nil {
			break
		}
		sPort, errS := strconv.Atoi(sPortStr)
		dPort, errD := strconv.Atoi(dPortStr)
		if errS != nil || errD != nil || sPort < 1 || sPort > 65535 || dPort < 1 || dPort > 65535 {
			break
		}
		return sIP, uint16(sPort), dIP, uint16(dPort)
	}
	return nil, 0, nil, 0
}
