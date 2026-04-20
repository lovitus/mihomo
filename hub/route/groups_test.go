package route

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/metacubex/http"
	"github.com/metacubex/http/httptest"
	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/adapter/outboundgroup"
	"github.com/metacubex/mihomo/common/utils"
	C "github.com/metacubex/mihomo/constant"
	P "github.com/metacubex/mihomo/constant/provider"
)

type mockGroupAdapter struct {
	persistent bool
	forceSetTo []string
}

func (m *mockGroupAdapter) Name() string           { return "group" }
func (m *mockGroupAdapter) Type() C.AdapterType    { return C.Fallback }
func (m *mockGroupAdapter) Addr() string           { return "" }
func (m *mockGroupAdapter) SupportUDP() bool       { return false }
func (m *mockGroupAdapter) ProxyInfo() C.ProxyInfo { return C.ProxyInfo{} }
func (m *mockGroupAdapter) SupportUOT() bool       { return false }
func (m *mockGroupAdapter) DialContext(context.Context, *C.Metadata) (C.Conn, error) {
	return nil, C.ErrNotSupport
}
func (m *mockGroupAdapter) ListenPacketContext(context.Context, *C.Metadata) (C.PacketConn, error) {
	return nil, C.ErrNotSupport
}
func (m *mockGroupAdapter) IsL3Protocol(*C.Metadata) bool    { return false }
func (m *mockGroupAdapter) Unwrap(*C.Metadata, bool) C.Proxy { return nil }
func (m *mockGroupAdapter) Close() error                     { return nil }
func (m *mockGroupAdapter) Providers() []P.ProxyProvider     { return nil }
func (m *mockGroupAdapter) Proxies() []C.Proxy               { return nil }
func (m *mockGroupAdapter) Now() string                      { return "group" }
func (m *mockGroupAdapter) Touch()                           {}
func (m *mockGroupAdapter) Hidden() bool                     { return false }
func (m *mockGroupAdapter) Icon() string                     { return "" }
func (m *mockGroupAdapter) Set(string) error                 { return nil }
func (m *mockGroupAdapter) PersistentPin() bool              { return m.persistent }
func (m *mockGroupAdapter) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"name": "group"})
}
func (m *mockGroupAdapter) URLTest(context.Context, string, utils.IntRanges[uint16]) (map[string]uint16, error) {
	return map[string]uint16{"group": 12}, nil
}
func (m *mockGroupAdapter) ForceSet(name string) {
	m.forceSetTo = append(m.forceSetTo, name)
}

var _ outboundgroup.ProxyGroup = (*mockGroupAdapter)(nil)
var _ outboundgroup.SelectAble = (*mockGroupAdapter)(nil)
var _ outboundgroup.PersistentPinAware = (*mockGroupAdapter)(nil)

func TestGetGroupDelayDoesNotClearPersistentPinSelection(t *testing.T) {
	adapterImpl := &mockGroupAdapter{persistent: true}
	proxy := adapter.NewProxy(adapterImpl)

	req, err := http.NewRequest("GET", "/groups/group/delay?url=https://example.com&timeout=1000&expected=", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req = req.WithContext(context.WithValue(req.Context(), CtxKeyProxy, proxy))
	rec := httptest.NewRecorder()

	getGroupDelay(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(adapterImpl.forceSetTo) != 0 {
		t.Fatalf("ForceSet() calls = %#v, want none", adapterImpl.forceSetTo)
	}
}

func TestGetGroupDelayClearsSelectionWhenPersistentPinDisabled(t *testing.T) {
	adapterImpl := &mockGroupAdapter{persistent: false}
	proxy := adapter.NewProxy(adapterImpl)

	req, err := http.NewRequest("GET", "/groups/group/delay?url=https://example.com&timeout=1000&expected=", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req = req.WithContext(context.WithValue(req.Context(), CtxKeyProxy, proxy))
	rec := httptest.NewRecorder()

	getGroupDelay(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(adapterImpl.forceSetTo) != 1 || adapterImpl.forceSetTo[0] != "" {
		t.Fatalf("ForceSet() calls = %#v, want one clear call", adapterImpl.forceSetTo)
	}
}
