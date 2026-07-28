package credential

import (
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"sync"
	"testing"
	"time"
)

func mustKey(t *testing.T) *ecdh.PrivateKey {
	t.Helper()
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

const testFront = "www.example-front.com"

var testShortID = [8]byte{9, 8, 7, 6, 5, 4, 3, 2}

// clock is a hand-driven time source, so the window and replay tests assert on
// exact boundaries instead of sleeping.
type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }

func newTestOpener(t *testing.T, priv *ecdh.PrivateKey, clk *clock) *Opener {
	t.Helper()
	o, err := NewOpener(OpenerConfig{
		PrivateKey: priv,
		Front:      testFront,
		ShortIDs:   [][8]byte{testShortID},
		now:        clk.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func validCred(clk *clock) Credential {
	return Credential{Version: Version1, Timestamp: clk.t, ShortID: testShortID}
}

func TestTokenRoundTrip(t *testing.T) {
	priv := mustKey(t)
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	o := newTestOpener(t, priv, clk)

	tok, err := SealToken(priv.PublicKey(), testFront, validCred(clk))
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != TokenLen {
		t.Fatalf("token 长度 %d，期望 %d", len(tok), TokenLen)
	}

	got, err := o.OpenToken(tok)
	if err != nil {
		t.Fatalf("合法凭据应通过: %v", err)
	}
	if got.ShortID != testShortID {
		t.Errorf("short_id = %x，期望 %x", got.ShortID, testShortID)
	}
	if got.Version != Version1 {
		t.Errorf("version = %#x，期望 %#x", got.Version, Version1)
	}
	// The wire format keeps minute precision, so equality is on the truncation.
	if want := time.Unix(clk.t.Unix()/60*60, 0); !got.Timestamp.Equal(want) {
		t.Errorf("timestamp = %v，期望 %v", got.Timestamp, want)
	}
}

// TestOnlyOurClientsOpen covers spec §4.5 steps 3-4: everything an outsider can
// produce must land on the borrowed-shell path.
func TestOnlyOurClientsOpen(t *testing.T) {
	priv := mustKey(t)
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	o := newTestOpener(t, priv, clk)

	good, err := SealToken(priv.PublicKey(), testFront, validCred(clk))
	if err != nil {
		t.Fatal(err)
	}
	otherNode, err := SealToken(mustKey(t).PublicKey(), testFront, validCred(clk))
	if err != nil {
		t.Fatal(err)
	}
	otherFront, err := SealToken(priv.PublicKey(), "other.front.invalid", validCred(clk))
	if err != nil {
		t.Fatal(err)
	}

	flipLast := append([]byte(nil), good...)
	flipLast[len(flipLast)-1] ^= 0x01
	flipPub := append([]byte(nil), good...)
	flipPub[0] ^= 0x01

	for _, tc := range []struct {
		name  string
		token []byte
	}{
		{"全零 token", make([]byte, TokenLen)},
		{"对着别的节点公钥生成", otherNode},
		{"绑定了别的 front", otherFront},
		{"密文最后一字节翻转", flipLast},
		{"客户端公钥被改", flipPub},
		{"太短", good[:TokenLen-1]},
		{"太长", append(append([]byte(nil), good...), 0)},
		{"空", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := o.OpenToken(tc.token); !errors.Is(err, ErrNotOurs) {
				t.Fatalf("应判为借壳路径，得到 err=%v", err)
			}
		})
	}

	// Control arm: without it, a broken Open that rejects everything would pass
	// every case above.
	if _, err := o.OpenToken(good); err != nil {
		t.Fatalf("对照组：合法 token 应通过，得到 %v", err)
	}
}

// TestTimestampWindow covers §4.5 step 5. Note the boundary cases: the wire
// format truncates to minutes, so a credential minted "now" can read as up to
// 59s in the past.
func TestTimestampWindow(t *testing.T) {
	priv := mustKey(t)
	base := time.Unix(1_700_000_000, 0).Truncate(time.Minute)

	for _, tc := range []struct {
		name   string
		skew   time.Duration // how far the node's clock is ahead of the client's
		wantOK bool
	}{
		{"同刻", 0, true},
		{"客户端早 9 分钟", 9 * time.Minute, true},
		{"客户端早 10 分钟（边界内）", DefaultReplayWindow, true},
		{"客户端早 11 分钟", 11 * time.Minute, false},
		{"客户端晚 9 分钟（节点时钟慢）", -9 * time.Minute, true},
		{"客户端晚 11 分钟", -11 * time.Minute, false},
		{"客户端早一整天", 24 * time.Hour, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clk := &clock{t: base}
			o := newTestOpener(t, priv, clk)
			tok, err := SealToken(priv.PublicKey(), testFront, Credential{
				Version: Version1, Timestamp: base, ShortID: testShortID,
			})
			if err != nil {
				t.Fatal(err)
			}
			clk.t = base.Add(tc.skew)

			_, err = o.OpenToken(tok)
			if tc.wantOK && err != nil {
				t.Fatalf("应通过，得到 %v", err)
			}
			if !tc.wantOK && !errors.Is(err, ErrNotOurs) {
				t.Fatalf("应判为借壳路径，得到 err=%v", err)
			}
		})
	}
}

func TestUnknownVersionIsRejected(t *testing.T) {
	priv := mustKey(t)
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	o := newTestOpener(t, priv, clk)

	tok, err := SealToken(priv.PublicKey(), testFront, Credential{
		Version: 0x02, Timestamp: clk.t, ShortID: testShortID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.OpenToken(tok); !errors.Is(err, ErrNotOurs) {
		t.Fatalf("未知版本应判为借壳路径，得到 err=%v", err)
	}
}

func TestShortIDMustBeServedHere(t *testing.T) {
	priv := mustKey(t)
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	o := newTestOpener(t, priv, clk)

	tok, err := SealToken(priv.PublicKey(), testFront, Credential{
		Version: Version1, Timestamp: clk.t, ShortID: [8]byte{1, 1, 1, 1, 1, 1, 1, 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.OpenToken(tok); !errors.Is(err, ErrNotOurs) {
		t.Fatalf("未注册的 short_id 应判为借壳路径，得到 err=%v", err)
	}
}

// TestNodeWithNoShortIDsAuthenticatesNobody pins the direction of the empty-set
// default. Reading it as "no restriction configured" would turn a
// misconfiguration into an open node.
func TestNodeWithNoShortIDsAuthenticatesNobody(t *testing.T) {
	priv := mustKey(t)
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	o, err := NewOpener(OpenerConfig{PrivateKey: priv, Front: testFront, now: clk.now})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := SealToken(priv.PublicKey(), testFront, validCred(clk))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.OpenToken(tok); !errors.Is(err, ErrNotOurs) {
		t.Fatalf("未配置 short_id 的节点不应认证任何人，得到 err=%v", err)
	}
}

func TestReplayIsRejected(t *testing.T) {
	priv := mustKey(t)
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	o := newTestOpener(t, priv, clk)

	tok, err := SealToken(priv.PublicKey(), testFront, validCred(clk))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.OpenToken(tok); err != nil {
		t.Fatalf("首次应通过: %v", err)
	}
	if _, err := o.OpenToken(tok); !errors.Is(err, ErrNotOurs) {
		t.Fatalf("重放应判为借壳路径，得到 err=%v", err)
	}
}

// TestReplayCacheOutlivesTheAcceptanceWindow is the reason retention is by time
// rather than by capacity. If an entry could drop while its credential was
// still inside the ± window, replaying it in that gap would work.
func TestReplayCacheOutlivesTheAcceptanceWindow(t *testing.T) {
	priv := mustKey(t)
	base := time.Unix(1_700_000_000, 0).Truncate(time.Minute)
	clk := &clock{t: base}
	o := newTestOpener(t, priv, clk)

	// Mint with the longest possible remaining life: a client whose clock runs
	// ahead by the full window future-dates its credential, and the node keeps
	// accepting that timestamp until it is a window old -- two windows from now.
	// Minting at the *earliest* acceptable instant instead would test nothing:
	// that credential goes stale a minute later on the timestamp check alone,
	// before the cache is ever consulted.
	tok, err := SealToken(priv.PublicKey(), testFront, Credential{
		Version: Version1, Timestamp: base.Add(DefaultReplayWindow), ShortID: testShortID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.OpenToken(tok); err != nil {
		t.Fatalf("首次应通过: %v", err)
	}

	// Walk the clock forward one minute at a time across the whole window. At
	// every step the credential must be refused -- either as a replay while it
	// is still fresh, or as stale once it ages out. There must be no instant
	// where it is neither.
	for elapsed := time.Minute; elapsed <= 2*DefaultReplayWindow; elapsed += time.Minute {
		clk.t = base.Add(elapsed)
		if _, err := o.OpenToken(tok); !errors.Is(err, ErrNotOurs) {
			t.Fatalf("首次之后 %v，重放被接受了（缓存与接受窗口之间有缝）", elapsed)
		}
	}
}

// TestReplayCacheFailsClosedWhenFull pins the overflow direction: a full cache
// must refuse rather than evict. Evicting would keep the node useful while
// making it confirmable by replay; refusing makes it useless but keeps it
// indistinguishable from a plain proxy of the front.
func TestReplayCacheFailsClosedWhenFull(t *testing.T) {
	priv := mustKey(t)
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	o, err := NewOpener(OpenerConfig{
		PrivateKey:       priv,
		Front:            testFront,
		ShortIDs:         [][8]byte{testShortID},
		MaxReplayEntries: 2,
		now:              clk.now,
	})
	if err != nil {
		t.Fatal(err)
	}

	mint := func() []byte {
		tok, err := SealToken(priv.PublicKey(), testFront, validCred(clk))
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	first, second, third := mint(), mint(), mint()

	if _, err := o.OpenToken(first); err != nil {
		t.Fatalf("第 1 个应通过: %v", err)
	}
	if _, err := o.OpenToken(second); err != nil {
		t.Fatalf("第 2 个应通过: %v", err)
	}
	if _, err := o.OpenToken(third); !errors.Is(err, ErrNotOurs) {
		t.Fatalf("缓存满时应拒绝（fail closed），得到 err=%v", err)
	}
	if o.Overflows() != 1 {
		t.Errorf("Overflows() = %d，期望 1 —— 运维需要看到这个信号", o.Overflows())
	}

	// Once the entries age out the node recovers on its own; a stuck-full cache
	// would be an outage nobody could clear without a restart.
	clk.add(2*DefaultReplayWindow + time.Minute)
	fresh, err := SealToken(priv.PublicKey(), testFront, validCred(clk))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.OpenToken(fresh); err != nil {
		t.Fatalf("过期后应自行恢复: %v", err)
	}
}

// TestConcurrentReplayAdmitsExactlyOne guards the check-then-insert: two
// goroutines racing the same credential must not both win.
func TestConcurrentReplayAdmitsExactlyOne(t *testing.T) {
	priv := mustKey(t)
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	o := newTestOpener(t, priv, clk)

	tok, err := SealToken(priv.PublicKey(), testFront, validCred(clk))
	if err != nil {
		t.Fatal(err)
	}

	const racers = 32
	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted := 0
	start := make(chan struct{})
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := o.OpenToken(tok); err == nil {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	if accepted != 1 {
		t.Fatalf("%d 个并发重放中 %d 个被接受，期望恰好 1 个", racers, accepted)
	}
}

// TestEachConnectionGetsAFreshKey pins the premise that makes the deterministic
// nonce safe: the ephemeral key is per connection, so no (key, nonce) pair is
// ever used twice. Two seals of identical plaintext must differ.
func TestEachConnectionGetsAFreshKey(t *testing.T) {
	priv := mustKey(t)
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	c := validCred(clk)

	a, err := SealToken(priv.PublicKey(), testFront, c)
	if err != nil {
		t.Fatal(err)
	}
	b, err := SealToken(priv.PublicKey(), testFront, c)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) == string(b) {
		t.Fatal("相同明文两次封装得到相同字节 —— 临时密钥没有每连接重新生成，确定性 nonce 的前提被破坏")
	}
	if string(a[:PublicKeyLen]) == string(b[:PublicKeyLen]) {
		t.Fatal("两次封装的客户端公钥相同")
	}
}

func TestOpenerConfigIsValidated(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  OpenerConfig
	}{
		{"无私钥", OpenerConfig{Front: testFront}},
		{"空 front", OpenerConfig{PrivateKey: mustKey(t)}},
		{"负窗口", OpenerConfig{PrivateKey: mustKey(t), Front: testFront, ReplayWindow: -time.Minute}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewOpener(tc.cfg); err == nil {
				t.Fatal("应报错而非静默接受")
			}
		})
	}
}
