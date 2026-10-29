//go:build darwin && !ios

package provider

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"testing"
)

// Real-world check (opt-in via K2_REAL_LSOF=1): run the actual lsof sweep and prove the parser
// finds a socket we just opened ourselves. Not a unit test — it shells out.
func TestRealWorld_BuildConnIndexFindsOwnSocket(t *testing.T) {
	if os.Getenv("K2_REAL_LSOF") == "" {
		t.Skip("set K2_REAL_LSOF=1 to run")
	}
	// Open a real outbound TCP connection so lsof has something to find.
	c, err := net.Dial("tcp", "1.1.1.1:443")
	if err != nil {
		t.Skipf("no network: %v", err)
	}
	defer c.Close()
	la := c.LocalAddr().(*net.TCPAddr)
	ip, _ := netip.AddrFromSlice(la.IP)
	ip = ip.Unmap()

	s := NewDarwinProcessSearcher()
	idx := s.buildConnIndex()
	fmt.Printf("REAL: index size = %d entries\n", len(idx))
	if len(idx) == 0 {
		t.Fatal("REAL: lsof sweep produced an EMPTY index — parser or command is wrong")
	}
	key := cacheKey("tcp", ip, uint16(la.Port))
	got, ok := idx[key]
	fmt.Printf("REAL: our own socket %s -> %q (found=%v)\n", key, got, ok)
	// NOTE: selfPid is excluded by design, so our own socket must be ABSENT.
	// That is the discriminator: an index that contained it would mean the
	// self-filter is broken.
	if ok {
		t.Fatalf("REAL: own socket present as %q — selfPid filter is broken", got)
	}
	// Show a sample so a human can eyeball that commands/addresses look sane.
	n := 0
	for k, v := range idx {
		fmt.Printf("REAL sample: %-40s %s\n", k, v)
		if n++; n >= 8 {
			break
		}
	}
}
