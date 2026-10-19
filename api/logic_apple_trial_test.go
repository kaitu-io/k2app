package center

import (
	"testing"

	"github.com/wordgate/qtoolkit/appstore"
)

func TestClassifyAppleTxn(t *testing.T) {
	cases := []struct {
		name      string
		offerType int32
		price     int32
		want      appleTxnClass
	}{
		{"paid", 0, 4990, appleTxnPaid},
		{"paid, price field missing", 0, 0, appleTxnPaid},
		{"intro free trial", 1, 0, appleTxnFreeTrial},
		{"intro paid offer", 1, 990, appleTxnOtherOffer},
		{"promotional offer", 2, 0, appleTxnOtherOffer},
		{"offer code", 3, 0, appleTxnOtherOffer},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classifyAppleTxn(&appstore.TransactionInfo{OfferType: c.offerType, Price: c.price})
			if got != c.want {
				t.Fatalf("classifyAppleTxn(offerType=%d, price=%d) = %d, want %d", c.offerType, c.price, got, c.want)
			}
		})
	}
}
