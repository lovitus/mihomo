package outboundgroup

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/metacubex/mihomo/adapter/provider"
	"github.com/metacubex/mihomo/common/utils"
	C "github.com/metacubex/mihomo/constant"
	P "github.com/metacubex/mihomo/constant/provider"
)

type fakeURLTestResult struct {
	returnDelay  uint16
	historyDelay uint16
	alive        bool
	err          error
	at           time.Time
}

type fakeProxy struct {
	name      string
	tp        C.AdapterType
	udp       bool
	alive     map[string]bool
	histories map[string][]C.DelayHistory
	urlTests  map[string][]fakeURLTestResult
}

func newFakeProxy(name string) *fakeProxy {
	return &fakeProxy{
		name:      name,
		tp:        C.Shadowsocks,
		alive:     map[string]bool{},
		histories: map[string][]C.DelayHistory{},
		urlTests:  map[string][]fakeURLTestResult{},
	}
}

func (p *fakeProxy) Adapter() C.ProxyAdapter { return p }
func (p *fakeProxy) Name() string            { return p.name }
func (p *fakeProxy) Type() C.AdapterType     { return p.tp }
func (p *fakeProxy) Addr() string            { return "127.0.0.1:1" }
func (p *fakeProxy) SupportUDP() bool        { return p.udp }
func (p *fakeProxy) ProxyInfo() C.ProxyInfo  { return C.ProxyInfo{} }
func (p *fakeProxy) SupportUOT() bool        { return false }
func (p *fakeProxy) IsL3Protocol(*C.Metadata) bool {
	return false
}
func (p *fakeProxy) DialContext(context.Context, *C.Metadata) (C.Conn, error) {
	return nil, C.ErrNotSupport
}
func (p *fakeProxy) ListenPacketContext(context.Context, *C.Metadata) (C.PacketConn, error) {
	return nil, C.ErrNotSupport
}
func (p *fakeProxy) Unwrap(*C.Metadata, bool) C.Proxy { return p }
func (p *fakeProxy) Close() error                     { return nil }
func (p *fakeProxy) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"name": p.name, "type": p.tp.String()})
}
func (p *fakeProxy) AliveForTestUrl(url string) bool {
	return p.alive[url]
}
func (p *fakeProxy) DelayHistory() []C.DelayHistory {
	return nil
}
func (p *fakeProxy) ExtraDelayHistories() map[string]C.ProxyState {
	out := make(map[string]C.ProxyState, len(p.histories))
	for url, history := range p.histories {
		copied := append([]C.DelayHistory(nil), history...)
		out[url] = C.ProxyState{Alive: p.alive[url], History: copied}
	}
	return out
}
func (p *fakeProxy) LastDelayForTestUrl(url string) uint16 {
	history := p.histories[url]
	if !p.alive[url] || len(history) == 0 {
		return 0xffff
	}
	last := history[len(history)-1]
	if last.Delay == 0 {
		return 0xffff
	}
	return last.Delay
}
func (p *fakeProxy) URLTest(_ context.Context, url string, _ utils.IntRanges[uint16]) (uint16, error) {
	results := p.urlTests[url]
	if len(results) == 0 {
		return 0, errors.New("no fake URLTest result configured")
	}
	result := results[0]
	p.urlTests[url] = results[1:]
	p.setState(url, result.alive, result.historyDelay, result.at)
	return result.returnDelay, result.err
}

func (p *fakeProxy) setState(url string, alive bool, delay uint16, at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	p.alive[url] = alive
	p.histories[url] = append(p.histories[url], C.DelayHistory{Time: at, Delay: delay})
}

func (p *fakeProxy) setSequence(url string, results ...fakeURLTestResult) {
	p.urlTests[url] = append([]fakeURLTestResult(nil), results...)
}

func newTestProvider(t *testing.T, name string, proxies ...C.Proxy) P.ProxyProvider {
	t.Helper()

	hc := provider.NewHealthCheck(proxies, "https://example.com", 0, 0, false, nil)
	pd, err := provider.NewCompatibleProvider(name, proxies, hc)
	if err != nil {
		t.Fatalf("NewCompatibleProvider() error = %v", err)
	}
	return pd
}

func decodeJSONMap(t *testing.T, data []byte) map[string]any {
	t.Helper()

	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	return out
}

func TestGroupBaseURLTestFiltersUnhealthyResults(t *testing.T) {
	const testURL = "https://example.com"
	bad := newFakeProxy("bad")
	bad.setSequence(testURL, fakeURLTestResult{
		returnDelay:  20,
		historyDelay: 0,
		alive:        false,
		at:           time.Unix(10, 0),
	})
	good := newFakeProxy("good")
	good.setSequence(testURL, fakeURLTestResult{
		returnDelay:  15,
		historyDelay: 15,
		alive:        true,
		at:           time.Unix(11, 0),
	})

	gb := NewGroupBase(GroupBaseOption{
		Name:      "g",
		Type:      C.URLTest,
		Providers: []P.ProxyProvider{newTestProvider(t, "p", bad, good)},
	})

	delays, err := gb.URLTest(context.Background(), testURL, nil)
	if err != nil {
		t.Fatalf("URLTest() error = %v", err)
	}
	if len(delays) != 1 || delays["good"] != 15 {
		t.Fatalf("URLTest() delays = %#v, want only healthy proxy result", delays)
	}
}

func TestFallbackClearsSelectionWhenPersistentPinDisabled(t *testing.T) {
	const testURL = "https://example.com"
	pinned := newFakeProxy("pinned")
	pinned.setState(testURL, false, 0, time.Unix(1, 0))
	alive := newFakeProxy("alive")
	alive.setState(testURL, true, 30, time.Unix(1, 0))

	fb := NewFallback(&GroupCommonOption{
		Name:           "fallback",
		URL:            testURL,
		ExpectedStatus: "*",
	}, []P.ProxyProvider{newTestProvider(t, "p", pinned, alive)})
	fb.selected = "pinned"

	got := fb.findAliveProxy(false)
	if got.Name() != "alive" {
		t.Fatalf("findAliveProxy() = %s, want alive", got.Name())
	}
	if fb.getSelected() != "" {
		t.Fatalf("selected = %q, want cleared", fb.getSelected())
	}
}

func TestFallbackKeepsPinnedProxyUntilThresholdReached(t *testing.T) {
	const testURL = "https://example.com"
	pinned := newFakeProxy("pinned")
	pinned.setState(testURL, false, 0, time.Unix(2, 0))
	alive := newFakeProxy("alive")
	alive.setState(testURL, true, 20, time.Unix(2, 0))

	fb := NewFallback(&GroupCommonOption{
		Name:                            "fallback",
		URL:                             testURL,
		ExpectedStatus:                  "*",
		PersistentPin:                   true,
		PersistentPinAutoUnfixThreshold: 2,
	}, []P.ProxyProvider{newTestProvider(t, "p", pinned, alive)})
	fb.setSelected("pinned")

	got := fb.findAliveProxy(false)
	if got.Name() != "pinned" {
		t.Fatalf("findAliveProxy() = %s, want pinned before threshold", got.Name())
	}
	if fb.pinAutoUnfixCount != 1 {
		t.Fatalf("pinAutoUnfixCount = %d, want 1", fb.pinAutoUnfixCount)
	}
	if fb.getSelected() != "pinned" {
		t.Fatalf("selected = %q, want pinned", fb.getSelected())
	}

	pinned.setState(testURL, false, 0, time.Unix(3, 0))
	got = fb.findAliveProxy(false)
	if got.Name() != "alive" {
		t.Fatalf("findAliveProxy() after threshold = %s, want alive", got.Name())
	}
	if fb.getSelected() != "" {
		t.Fatalf("selected = %q, want cleared after auto-unfix", fb.getSelected())
	}
}

func TestFallbackResetsCounterWithoutAlternativeAliveProxy(t *testing.T) {
	const testURL = "https://example.com"
	pinned := newFakeProxy("pinned")
	pinned.setState(testURL, false, 0, time.Unix(4, 0))
	other := newFakeProxy("other")
	other.setState(testURL, false, 0, time.Unix(4, 0))

	fb := NewFallback(&GroupCommonOption{
		Name:                            "fallback",
		URL:                             testURL,
		ExpectedStatus:                  "*",
		PersistentPin:                   true,
		PersistentPinAutoUnfixThreshold: 5,
	}, []P.ProxyProvider{newTestProvider(t, "p", pinned, other)})
	fb.setSelected("pinned")
	fb.pinAutoUnfixCount = 2
	fb.pinAutoUnfixLastTest = time.Unix(3, 0)

	got := fb.findAliveProxy(false)
	if got.Name() != "pinned" {
		t.Fatalf("findAliveProxy() = %s, want pinned when no alternative alive proxy", got.Name())
	}
	if fb.pinAutoUnfixCount != 0 {
		t.Fatalf("pinAutoUnfixCount = %d, want reset to 0", fb.pinAutoUnfixCount)
	}
}

func TestFallbackClearsMissingPersistentPinAndFallsBackToFirstAlive(t *testing.T) {
	const testURL = "https://example.com"
	firstAlive := newFakeProxy("first-alive")
	firstAlive.setState(testURL, true, 40, time.Unix(5, 0))
	secondAlive := newFakeProxy("second-alive")
	secondAlive.setState(testURL, true, 20, time.Unix(5, 0))

	fb := NewFallback(&GroupCommonOption{
		Name:           "fallback",
		URL:            testURL,
		ExpectedStatus: "*",
		PersistentPin:  true,
	}, []P.ProxyProvider{newTestProvider(t, "p", firstAlive, secondAlive)})
	fb.setSelected("missing")

	got := fb.findAliveProxy(false)
	if got.Name() != "first-alive" {
		t.Fatalf("findAliveProxy() = %s, want first-alive", got.Name())
	}
	if fb.getSelected() != "" {
		t.Fatalf("selected = %q, want cleared", fb.getSelected())
	}
}

func TestFallbackResetsCounterWhenPinnedProxyRecovers(t *testing.T) {
	const testURL = "https://example.com"
	pinned := newFakeProxy("pinned")
	pinned.setState(testURL, true, 12, time.Unix(7, 0))
	alive := newFakeProxy("alive")
	alive.setState(testURL, true, 30, time.Unix(7, 0))

	fb := NewFallback(&GroupCommonOption{
		Name:                            "fallback",
		URL:                             testURL,
		ExpectedStatus:                  "*",
		PersistentPin:                   true,
		PersistentPinAutoUnfixThreshold: 3,
	}, []P.ProxyProvider{newTestProvider(t, "p", pinned, alive)})
	fb.setSelected("pinned")
	fb.pinAutoUnfixCount = 2
	fb.pinAutoUnfixLastTest = time.Unix(6, 0)

	got := fb.findAliveProxy(false)
	if got.Name() != "pinned" {
		t.Fatalf("findAliveProxy() = %s, want pinned", got.Name())
	}
	if fb.pinAutoUnfixCount != 0 {
		t.Fatalf("pinAutoUnfixCount = %d, want reset to 0", fb.pinAutoUnfixCount)
	}
	if fb.getSelected() != "pinned" {
		t.Fatalf("selected = %q, want pinned", fb.getSelected())
	}
}

func TestFallbackResetsCounterWhenPinnedProxyHasSuccessfulTestRecords(t *testing.T) {
	const testURL = "https://example.com"
	pinned := newFakeProxy("pinned")
	pinned.setState(testURL, false, 0, time.Unix(9, 0))
	pinned.setState(testURL, true, 25, time.Unix(10, 0))
	pinned.setState(testURL, false, 0, time.Unix(11, 0))
	alive := newFakeProxy("alive")
	alive.setState(testURL, true, 15, time.Unix(11, 0))

	fb := NewFallback(&GroupCommonOption{
		Name:                            "fallback",
		URL:                             testURL,
		ExpectedStatus:                  "*",
		PersistentPin:                   true,
		PersistentPinAutoUnfixThreshold: 3,
	}, []P.ProxyProvider{newTestProvider(t, "p", pinned, alive)})
	fb.setSelected("pinned")
	fb.pinAutoUnfixCount = 2
	fb.pinAutoUnfixLastTest = time.Unix(8, 0)

	got := fb.findAliveProxy(false)
	if got.Name() != "pinned" {
		t.Fatalf("findAliveProxy() = %s, want pinned", got.Name())
	}
	if fb.pinAutoUnfixCount != 1 {
		t.Fatalf("pinAutoUnfixCount = %d, want reset then increment to 1", fb.pinAutoUnfixCount)
	}
	if fb.getSelected() != "pinned" {
		t.Fatalf("selected = %q, want pinned", fb.getSelected())
	}
}

func TestFallbackMarshalJSONIncludesPersistentPinFields(t *testing.T) {
	const testURL = "https://example.com"
	pinned := newFakeProxy("pinned")
	pinned.setState(testURL, true, 10, time.Unix(12, 0))

	fb := NewFallback(&GroupCommonOption{
		Name:                            "fallback",
		URL:                             testURL,
		ExpectedStatus:                  "*",
		PersistentPin:                   true,
		PinUnhealthyLogInterval:         13,
		PersistentPinAutoUnfixThreshold: 7,
	}, []P.ProxyProvider{newTestProvider(t, "p", pinned)})
	fb.setSelected("pinned")

	data, err := fb.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	out := decodeJSONMap(t, data)

	if got := out["fixed"]; got != "pinned" {
		t.Fatalf("fixed = %#v, want pinned", got)
	}
	if got := out["persistentPin"]; got != true {
		t.Fatalf("persistentPin = %#v, want true", got)
	}
	if got := out["pinUnhealthyLogInterval"]; got != float64(13) {
		t.Fatalf("pinUnhealthyLogInterval = %#v, want 13", got)
	}
	if got := out["persistentPinAutoUnfixThreshold"]; got != float64(7) {
		t.Fatalf("persistentPinAutoUnfixThreshold = %#v, want 7", got)
	}
}

func TestURLTestPrefersAliveProxyAndRefreshesAfterManualURLTest(t *testing.T) {
	const testURL = "https://example.com"
	slow := newFakeProxy("slow")
	slow.setState(testURL, true, 100, time.Unix(10, 0))
	timeout := newFakeProxy("timeout")
	timeout.setState(testURL, false, 0, time.Unix(10, 0))
	fast := newFakeProxy("fast")
	fast.setState(testURL, true, 80, time.Unix(10, 0))

	group := NewURLTest(&GroupCommonOption{
		Name:           "auto",
		URL:            testURL,
		ExpectedStatus: "*",
	}, []P.ProxyProvider{newTestProvider(t, "p", slow, timeout, fast)})

	if got := group.fast(false).Name(); got != "fast" {
		t.Fatalf("fast() = %s, want fastest alive proxy", got)
	}

	slow.setSequence(testURL, fakeURLTestResult{
		returnDelay:  300,
		historyDelay: 300,
		alive:        true,
		at:           time.Unix(11, 0),
	})
	timeout.setSequence(testURL, fakeURLTestResult{
		returnDelay:  999,
		historyDelay: 0,
		alive:        false,
		at:           time.Unix(11, 0),
	})
	fast.setSequence(testURL, fakeURLTestResult{
		returnDelay:  50,
		historyDelay: 50,
		alive:        true,
		at:           time.Unix(11, 0),
	})

	if _, err := group.URLTest(context.Background(), testURL, nil); err != nil {
		t.Fatalf("URLTest() error = %v", err)
	}
	if got := group.Now(); got != "fast" {
		t.Fatalf("Now() after URLTest = %s, want refreshed fastest alive proxy", got)
	}
}

func TestURLTestKeepsPinnedProxyUntilThresholdReached(t *testing.T) {
	const testURL = "https://example.com"
	pinned := newFakeProxy("pinned")
	pinned.setState(testURL, false, 0, time.Unix(20, 0))
	alive := newFakeProxy("alive")
	alive.setState(testURL, true, 25, time.Unix(20, 0))

	group := NewURLTest(&GroupCommonOption{
		Name:                            "auto",
		URL:                             testURL,
		ExpectedStatus:                  "*",
		PersistentPin:                   true,
		PersistentPinAutoUnfixThreshold: 2,
	}, []P.ProxyProvider{newTestProvider(t, "p", pinned, alive)})
	group.setSelected("pinned")

	if got := group.fast(false).Name(); got != "pinned" {
		t.Fatalf("fast() = %s, want pinned before threshold", got)
	}
	if group.pinAutoUnfixCount != 1 {
		t.Fatalf("pinAutoUnfixCount = %d, want 1", group.pinAutoUnfixCount)
	}

	pinned.setState(testURL, false, 0, time.Unix(21, 0))
	group.fastSingle.Reset()
	if got := group.fast(false).Name(); got != "alive" {
		t.Fatalf("fast() after threshold = %s, want alive", got)
	}
	if group.getSelected() != "" {
		t.Fatalf("selected = %q, want cleared after auto-unfix", group.getSelected())
	}
}

func TestURLTestClearsMissingPersistentPinAndSelectsFastestAlive(t *testing.T) {
	const testURL = "https://example.com"
	slow := newFakeProxy("slow")
	slow.setState(testURL, true, 80, time.Unix(22, 0))
	fast := newFakeProxy("fast")
	fast.setState(testURL, true, 20, time.Unix(22, 0))

	group := NewURLTest(&GroupCommonOption{
		Name:           "auto",
		URL:            testURL,
		ExpectedStatus: "*",
		PersistentPin:  true,
	}, []P.ProxyProvider{newTestProvider(t, "p", slow, fast)})
	group.setSelected("missing")

	if got := group.fast(false).Name(); got != "fast" {
		t.Fatalf("fast() = %s, want fast", got)
	}
	if group.getSelected() != "" {
		t.Fatalf("selected = %q, want cleared", group.getSelected())
	}
}

func TestURLTestResetsCounterWhenPinnedProxyRecovers(t *testing.T) {
	const testURL = "https://example.com"
	pinned := newFakeProxy("pinned")
	pinned.setState(testURL, true, 18, time.Unix(24, 0))
	alive := newFakeProxy("alive")
	alive.setState(testURL, true, 30, time.Unix(24, 0))

	group := NewURLTest(&GroupCommonOption{
		Name:                            "auto",
		URL:                             testURL,
		ExpectedStatus:                  "*",
		PersistentPin:                   true,
		PersistentPinAutoUnfixThreshold: 4,
	}, []P.ProxyProvider{newTestProvider(t, "p", pinned, alive)})
	group.setSelected("pinned")
	group.pinAutoUnfixCount = 3
	group.pinAutoUnfixLastTest = time.Unix(23, 0)

	if got := group.fast(false).Name(); got != "pinned" {
		t.Fatalf("fast() = %s, want pinned", got)
	}
	if group.pinAutoUnfixCount != 0 {
		t.Fatalf("pinAutoUnfixCount = %d, want reset to 0", group.pinAutoUnfixCount)
	}
	if group.getSelected() != "pinned" {
		t.Fatalf("selected = %q, want pinned", group.getSelected())
	}
}

func TestURLTestResetsCounterWhenPinnedProxyHasSuccessfulTestRecords(t *testing.T) {
	const testURL = "https://example.com"
	pinned := newFakeProxy("pinned")
	pinned.setState(testURL, false, 0, time.Unix(26, 0))
	pinned.setState(testURL, true, 15, time.Unix(27, 0))
	pinned.setState(testURL, false, 0, time.Unix(28, 0))
	alive := newFakeProxy("alive")
	alive.setState(testURL, true, 12, time.Unix(28, 0))

	group := NewURLTest(&GroupCommonOption{
		Name:                            "auto",
		URL:                             testURL,
		ExpectedStatus:                  "*",
		PersistentPin:                   true,
		PersistentPinAutoUnfixThreshold: 4,
	}, []P.ProxyProvider{newTestProvider(t, "p", pinned, alive)})
	group.setSelected("pinned")
	group.pinAutoUnfixCount = 3
	group.pinAutoUnfixLastTest = time.Unix(25, 0)

	if got := group.fast(false).Name(); got != "pinned" {
		t.Fatalf("fast() = %s, want pinned", got)
	}
	if group.pinAutoUnfixCount != 1 {
		t.Fatalf("pinAutoUnfixCount = %d, want reset then increment to 1", group.pinAutoUnfixCount)
	}
	if group.getSelected() != "pinned" {
		t.Fatalf("selected = %q, want pinned", group.getSelected())
	}
}

func TestURLTestMarshalJSONIncludesPersistentPinFields(t *testing.T) {
	const testURL = "https://example.com"
	pinned := newFakeProxy("pinned")
	pinned.setState(testURL, true, 11, time.Unix(29, 0))

	group := NewURLTest(&GroupCommonOption{
		Name:                            "auto",
		URL:                             testURL,
		ExpectedStatus:                  "*",
		PersistentPin:                   true,
		PinUnhealthyLogInterval:         14,
		PersistentPinAutoUnfixThreshold: 9,
	}, []P.ProxyProvider{newTestProvider(t, "p", pinned)})
	group.setSelected("pinned")

	data, err := group.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	out := decodeJSONMap(t, data)

	if got := out["fixed"]; got != "pinned" {
		t.Fatalf("fixed = %#v, want pinned", got)
	}
	if got := out["persistentPin"]; got != true {
		t.Fatalf("persistentPin = %#v, want true", got)
	}
	if got := out["pinUnhealthyLogInterval"]; got != float64(14) {
		t.Fatalf("pinUnhealthyLogInterval = %#v, want 14", got)
	}
	if got := out["persistentPinAutoUnfixThreshold"]; got != float64(9) {
		t.Fatalf("persistentPinAutoUnfixThreshold = %#v, want 9", got)
	}
}

func TestParseProxyGroupKeepsDefaultPinWarnIntervalWhenExplicitZero(t *testing.T) {
	config := map[string]any{
		"name":                       "fallback",
		"type":                       "fallback",
		"proxies":                    []string{"p1"},
		"url":                        "https://example.com",
		"persistent-pin":             true,
		"pin-unhealthy-log-interval": 0,
	}
	p1 := newFakeProxy("p1")
	group, err := ParseProxyGroup(config, map[string]C.Proxy{"p1": p1}, map[string]P.ProxyProvider{}, nil, nil)
	if err != nil {
		t.Fatalf("ParseProxyGroup() error = %v", err)
	}

	fb, ok := group.(*Fallback)
	if !ok {
		t.Fatalf("group type = %T, want *Fallback", group)
	}
	if !fb.persistentPin {
		t.Fatalf("persistentPin = false, want true")
	}
	if fb.pinWarnInterval != 10*time.Second {
		t.Fatalf("pinWarnInterval = %v, want 10s", fb.pinWarnInterval)
	}
}

func TestParseProxyGroupFallsBackToDefaultPinWarnIntervalWhenNegative(t *testing.T) {
	config := map[string]any{
		"name":                       "fallback",
		"type":                       "fallback",
		"proxies":                    []string{"p1"},
		"url":                        "https://example.com",
		"persistent-pin":             true,
		"pin-unhealthy-log-interval": -1,
	}
	p1 := newFakeProxy("p1")
	group, err := ParseProxyGroup(config, map[string]C.Proxy{"p1": p1}, map[string]P.ProxyProvider{}, nil, nil)
	if err != nil {
		t.Fatalf("ParseProxyGroup() error = %v", err)
	}

	fb, ok := group.(*Fallback)
	if !ok {
		t.Fatalf("group type = %T, want *Fallback", group)
	}
	if fb.pinWarnInterval != 10*time.Second {
		t.Fatalf("pinWarnInterval = %v, want 10s", fb.pinWarnInterval)
	}
}

func TestParseProxyGroupUsesDefaultAutoUnfixThresholdWhenUnset(t *testing.T) {
	config := map[string]any{
		"name":           "fallback",
		"type":           "fallback",
		"proxies":        []string{"p1"},
		"url":            "https://example.com",
		"persistent-pin": true,
	}
	p1 := newFakeProxy("p1")
	group, err := ParseProxyGroup(config, map[string]C.Proxy{"p1": p1}, map[string]P.ProxyProvider{}, nil, nil)
	if err != nil {
		t.Fatalf("ParseProxyGroup() error = %v", err)
	}

	fb, ok := group.(*Fallback)
	if !ok {
		t.Fatalf("group type = %T, want *Fallback", group)
	}
	if fb.pinAutoUnfixThreshold != defaultPersistentPinAutoUnfixThreshold {
		t.Fatalf("pinAutoUnfixThreshold = %d, want %d", fb.pinAutoUnfixThreshold, defaultPersistentPinAutoUnfixThreshold)
	}
}

func TestParseProxyGroupFallsBackToDefaultAutoUnfixThresholdWhenExplicitZero(t *testing.T) {
	config := map[string]any{
		"name":                                "fallback",
		"type":                                "fallback",
		"proxies":                             []string{"p1"},
		"url":                                 "https://example.com",
		"persistent-pin":                      true,
		"persistent-pin-auto-unfix-threshold": 0,
	}
	p1 := newFakeProxy("p1")
	group, err := ParseProxyGroup(config, map[string]C.Proxy{"p1": p1}, map[string]P.ProxyProvider{}, nil, nil)
	if err != nil {
		t.Fatalf("ParseProxyGroup() error = %v", err)
	}

	fb, ok := group.(*Fallback)
	if !ok {
		t.Fatalf("group type = %T, want *Fallback", group)
	}
	if fb.pinAutoUnfixThreshold != defaultPersistentPinAutoUnfixThreshold {
		t.Fatalf("pinAutoUnfixThreshold = %d, want %d", fb.pinAutoUnfixThreshold, defaultPersistentPinAutoUnfixThreshold)
	}
}
