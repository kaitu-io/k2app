package rule

import (
	"reflect"
	"testing"
)

func TestSplitGlobs(t *testing.T) {
	tests := []struct {
		name       string
		input      []string
		wantExact  []string
		wantPrefix []string
	}{
		{
			name:       "empty",
			input:      nil,
			wantExact:  nil,
			wantPrefix: nil,
		},
		{
			name:       "empty-slice",
			input:      []string{},
			wantExact:  nil,
			wantPrefix: nil,
		},
		{
			name:       "all-exact",
			input:      []string{"WeChat", "QQ"},
			wantExact:  []string{"WeChat", "QQ"},
			wantPrefix: nil,
		},
		{
			name:       "all-prefix",
			input:      []string{"WeChat*", "QQ*"},
			wantExact:  nil,
			wantPrefix: []string{"WeChat", "QQ"},
		},
		{
			name:       "mixed",
			input:      []string{"WeChat", "QQ*"},
			wantExact:  []string{"WeChat"},
			wantPrefix: []string{"QQ"},
		},
		{
			name:       "middle-star-skipped",
			input:      []string{"Foo*Bar"},
			wantExact:  nil,
			wantPrefix: nil,
		},
		{
			name:       "leading-star-skipped",
			input:      []string{"*Foo"},
			wantExact:  nil,
			wantPrefix: nil,
		},
		{
			name:       "just-star-skipped",
			input:      []string{"*"},
			wantExact:  nil,
			wantPrefix: nil,
		},
		{
			name:       "empty-string-skipped",
			input:      []string{""},
			wantExact:  nil,
			wantPrefix: nil,
		},
		{
			name:       "reverse-dns-prefix",
			input:      []string{"com.tencent.*", "com.eg.android.AlipayGphone"},
			wantExact:  []string{"com.eg.android.AlipayGphone"},
			wantPrefix: []string{"com.tencent."},
		},
		{
			name:       "order-preserved",
			input:      []string{"A", "B*", "C", "D*"},
			wantExact:  []string{"A", "C"},
			wantPrefix: []string{"B", "D"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotExact, gotPrefix := SplitGlobs(tt.input)
			if !reflect.DeepEqual(gotExact, tt.wantExact) {
				t.Errorf("exact: got %#v, want %#v", gotExact, tt.wantExact)
			}
			if !reflect.DeepEqual(gotPrefix, tt.wantPrefix) {
				t.Errorf("prefix: got %#v, want %#v", gotPrefix, tt.wantPrefix)
			}
		})
	}
}
