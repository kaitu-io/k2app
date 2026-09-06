package k2cc

import (
	"math"
	"reflect"
	"testing"
	"time"
)

func TestHistAdd_FirstBucketWhoseEdgeCovers(t *testing.T) {
	var counts [rateHistBuckets]uint64
	for _, v := range []float64{0, 1, 1.5, 30, 499, 500, 501, 1e9} {
		histAdd(counts[:], rateHistEdges[:], v)
	}
	// edges: 1,2,5,10,20,30,50,100,200,300,500,+Inf
	want := [rateHistBuckets]uint64{2, 1, 0, 0, 0, 1, 0, 0, 0, 0, 2, 2}
	if counts != want {
		t.Fatalf("counts=%v want %v", counts, want)
	}
}

func TestHistPercentile(t *testing.T) {
	var counts [rateHistBuckets]uint64
	if e, of := histPercentile(counts[:], rateHistEdges[:], 0.5); e != 0 || of {
		t.Fatalf("empty: got (%v,%v) want (0,false)", e, of)
	}
	counts[5] = 1 // single sample in the 30 bucket
	if e, of := histPercentile(counts[:], rateHistEdges[:], 0.5); e != 30 || of {
		t.Fatalf("n=1: got (%v,%v) want (30,false)", e, of)
	}
	counts = [rateHistBuckets]uint64{}
	counts[3] = 21 // 10
	counts[5] = 21 // 30
	if e, _ := histPercentile(counts[:], rateHistEdges[:], 0.5); e != 10 {
		t.Fatalf("p50 of 21×10 + 21×30: got %v want 10 (⌈0.5·42⌉=21st sample is in the 10 bucket)", e)
	}
	if e, _ := histPercentile(counts[:], rateHistEdges[:], 0.9); e != 30 {
		t.Fatalf("p90: got %v want 30", e)
	}
	counts = [rateHistBuckets]uint64{}
	counts[rateHistBuckets-1] = 3
	e, of := histPercentile(counts[:], rateHistEdges[:], 0.5)
	if e != 500 || !of {
		t.Fatalf("all overflow: got (%v,%v) want (500,true)", e, of)
	}
	var loss [lossHistBuckets]uint64
	loss[0] = 4 // loss exactly 0 lands in the 0 bucket
	if e, _ := histPercentile(loss[:], lossHistEdges[:], 0.9); e != 0 {
		t.Fatalf("zero-loss p90: got %v want 0", e)
	}
}

func TestFormatBucket(t *testing.T) {
	cases := []struct {
		edge float64
		of   bool
		want string
	}{
		{0, false, "0"}, {0.5, false, "0.5"}, {10, false, "10"}, {500, true, ">500"}, {1000, true, ">1000"},
	}
	for _, c := range cases {
		if got := FormatBucket(c.edge, c.of); got != c.want {
			t.Errorf("FormatBucket(%v,%v)=%q want %q", c.edge, c.of, got, c.want)
		}
	}
}

func TestCCSummary_SubSameEpoch(t *testing.T) {
	cur := CCSummary{SenderID: 9, MIs: 10, BytesSent: 100, LossMax: 0.4, TputMax: 7, BWEst: 5, Mode: K2CCModeDecision, MinRTT: 3 * time.Millisecond}
	cur.LossHist[2] = 6
	prev := CCSummary{SenderID: 9, MIs: 4, BytesSent: 30, LossMax: 0.9, TputMax: 9, BWEst: 1, Mode: K2CCModeSlowStart}
	prev.LossHist[2] = 2
	win := cur.Sub(prev)
	if win.MIs != 6 || win.BytesSent != 70 || win.LossHist[2] != 4 {
		t.Fatalf("counters not subtracted: %+v", win)
	}
	if win.LossMax != 0.4 || win.TputMax != 7 || win.BWEst != 5 || win.Mode != K2CCModeDecision || win.MinRTT != 3*time.Millisecond {
		t.Fatalf("current values must be the receiver's: %+v", win)
	}
	if win.SenderID != 9 {
		t.Fatalf("SenderID=%d want 9", win.SenderID)
	}
}

func TestCCSummary_SubDifferentEpochTreatsPrevAsZero(t *testing.T) {
	cur := CCSummary{SenderID: 2, MIs: 3}
	prev := CCSummary{SenderID: 1, MIs: 50}
	if win := cur.Sub(prev); win.MIs != 3 {
		t.Fatalf("new sender epoch: MIs=%d want 3 (prev must be ignored)", win.MIs)
	}
	if win := cur.Sub(CCSummary{}); win.MIs != 3 {
		t.Fatalf("zero prev: MIs=%d want 3", win.MIs)
	}
}

func TestCCSummary_SubSaturates(t *testing.T) {
	cur := CCSummary{SenderID: 1, MIs: 2}
	prev := CCSummary{SenderID: 1, MIs: 5}
	if win := cur.Sub(prev); win.MIs != 0 {
		t.Fatalf("saturating subtract: MIs=%d want 0", win.MIs)
	}
}

// A counter added to CCSummary but forgotten in Sub() would silently report
// lifetime totals inside every window. Walk the struct with reflect so this
// guard needs no per-field maintenance.
func TestCCSummary_SubCoversEveryCounterField(t *testing.T) {
	cur := CCSummary{SenderID: 7}
	prev := CCSummary{SenderID: 7}
	rc, rp := reflect.ValueOf(&cur).Elem(), reflect.ValueOf(&prev).Elem()
	for i := 0; i < rc.NumField(); i++ {
		f := rc.Type().Field(i)
		if f.Name == "SenderID" {
			continue
		}
		switch f.Type.Kind() {
		case reflect.Uint64:
			rc.Field(i).SetUint(5)
			rp.Field(i).SetUint(2)
		case reflect.Array:
			if f.Type.Elem().Kind() != reflect.Uint64 {
				continue
			}
			for j := 0; j < f.Type.Len(); j++ {
				rc.Field(i).Index(j).SetUint(5)
				rp.Field(i).Index(j).SetUint(2)
			}
		}
	}
	rw := reflect.ValueOf(cur.Sub(prev))
	for i := 0; i < rw.NumField(); i++ {
		f := rw.Type().Field(i)
		if f.Name == "SenderID" {
			continue
		}
		switch f.Type.Kind() {
		case reflect.Uint64:
			if got := rw.Field(i).Uint(); got != 3 {
				t.Errorf("Sub() ignores counter %s: got %d want 3", f.Name, got)
			}
		case reflect.Array:
			if f.Type.Elem().Kind() != reflect.Uint64 {
				continue
			}
			for j := 0; j < f.Type.Len(); j++ {
				if got := rw.Field(i).Index(j).Uint(); got != 3 {
					t.Errorf("Sub() ignores %s[%d]: got %d want 3", f.Name, j, got)
				}
			}
		}
	}
}

func TestHistEdges_LastLossEdgeCatchesClampedLoss(t *testing.T) {
	if lossHistEdges[lossHistBuckets-1] != 100 {
		t.Fatal("LossRate() is clamped to 1.0 → 100%; the last loss edge must be 100 so nothing overflows")
	}
	if !math.IsInf(rttHistEdges[rttHistBuckets-1], 1) || !math.IsInf(rateHistEdges[rateHistBuckets-1], 1) {
		t.Fatal("RTT and rate histograms must end with an open +Inf bucket")
	}
}
