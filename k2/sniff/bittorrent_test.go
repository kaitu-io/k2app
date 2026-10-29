package sniff

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestIsBitTorrent(t *testing.T) {
	t.Parallel()
	packets := []string{
		"13426974546f7272656e742070726f746f636f6c0000000000100000e21ea9569b69bab33c97851d0298bdfa89bc90922d5554313631302dea812fcd6a3563e3be40c1d1",
		"13426974546f7272656e742070726f746f636f6c00000000001000052aa4f5a7e209e54b32803d43670971c4c8caaa052d5452333030302d653369733079647675763638",
		"13426974546f7272656e742070726f746f636f6c00000000001000052aa4f5a7e209e54b32803d43670971c4c8caaa052d5452343035302d6f7a316c6e79377931716130",
	}
	for _, pkt := range packets {
		data, err := hex.DecodeString(pkt)
		if err != nil {
			t.Fatal(err)
		}
		if !IsBitTorrent(bytes.NewReader(data)) {
			t.Errorf("expected BT detection for packet %s...", pkt[:16])
		}
	}
}

func TestIsBitTorrent_Incomplete(t *testing.T) {
	t.Parallel()
	// Only 11 bytes — starts correct but truncated
	data, _ := hex.DecodeString("13426974546f7272656e74")
	if IsBitTorrent(bytes.NewReader(data)) {
		t.Error("incomplete packet should not be detected as BT")
	}
}

func TestIsBitTorrent_NotBT(t *testing.T) {
	t.Parallel()
	// Starts with 0x13 but wrong protocol string (last byte differs)
	data, _ := hex.DecodeString("13426974546f7272656e75")
	if IsBitTorrent(bytes.NewReader(data)) {
		t.Error("non-BT packet should not be detected")
	}
}

func TestIsBitTorrent_Empty(t *testing.T) {
	t.Parallel()
	if IsBitTorrent(bytes.NewReader(nil)) {
		t.Error("empty reader should not be detected as BT")
	}
}

func TestIsBitTorrent_WrongFirstByte(t *testing.T) {
	t.Parallel()
	if IsBitTorrent(bytes.NewReader([]byte{0x00, 0x01, 0x02})) {
		t.Error("wrong first byte should not be detected as BT")
	}
}

func TestIsUTP(t *testing.T) {
	t.Parallel()
	packets := []string{
		"010041a282d7ee7b583afb160004000006d8318da776968f92d666f7963f32dae23ba0d2c810d8b8209cc4939f54fde9eeaa521c2c20c9ba7f43f4fb0375f28de06643b5e3ca4685ab7ac76adca99783be72ef05ed59ef4234f5712b75b4c7c0d7bee8fe2ca20ad626ba5bb0ffcc16bf06790896f888048cf72716419a07db1a3dca4550fbcea75b53e97235168a221cf3e553dfbb723961bd719fab038d86e0ecb74747f5a2cd669de1c4b9ad375f3a492d09d98cdfad745435625401315bbba98d35d32086299801377b93495a63a9efddb8d05f5b37a5c5b1c0a25e917f12007bb5e05013ada8aff544fab8cadf61d80ddb0b60f12741e44515a109d144fd53ef845acb4b5ccf0d6fc302d7003d76df3fc3423bb0237301c9e88f900c2d392a8e0fdb36d143cf7527a93fd0a2638b746e72f6699fffcd4fd15348fce780d4caa04382fd9faf1ca0ae377ca805da7536662b84f5ee18dd3ae38fcb095a7543e55f9069ae92c8cf54ae44e97b558d35e2545c66601ed2149cbc32bd6df199a2be7cf0da8b2ff137e0d23e776bc87248425013876d3a3cc31a83b424b752bd0346437f24b532978005d8f5b1b0be1a37a2489c32a18a9ad3118e3f9d30eb299bffae18e1f0677c2a5c185e62519093fe6bc2b7339299ea50a587989f726ca6443a75dd5bb936f6367c6355d80fae53ff529d740b2e5576e3eefdf1fdbfc69c3c8d8ac750512635de63e054bee1d3b689bc1b2bc3d2601e42a00b5c89066d173d4ae7ffedfd2274e5cf6d868fbe640aedb69b8246142f00b32d459974287537ddd5373460dcbc92f5cfdd7a3ed6020822ae922d947893752ca1983d0d32977374c384ac8f5ab566859019b7351526b9f13e932037a55bb052d9deb3b3c23317e0784fdc51a64f2159bfea3b069cf5caf02ee2c3c1a6b6b427bb16165713e8802d95b5c8ed77953690e994bd38c9ae113fedaf6ee7fc2b96c032ceafc2a530ad0422e84546b9c6ad8ef6ea02fa508abddd1805c38a7b42e9b7c971b1b636865ebec06ed754bb404cd6b4e6cc8cb77bd4a0c43410d5cd5ef8fe853a66d49b3b9e06cb141236cdbfdd5761601dc54d1250b86c660e0f898fe62526fdd9acf0eab60a3bbbb2151970461f28f10b31689594bea646c4b03ee197d63bdef4e5a7c22716b3bb9494a83b78ecd81b338b80ac6c09c43485b1b09ba41c74343832c78f0520c1d659ac9eb1502094141e82fb9e5e620970ebc0655514c43c294a7714cbf9a499d277daf089f556398a01589a77494bec8bfb60a108f3813b55368672b88c1af40f6b3c8b513f7c70c3e0efce85228b8b9ec67ba0393f9f7305024d8e2da6a26cf85613d14f249170ce1000089df4c9c260df7f8292aa2ecb5d5bac97656d59aa248caedea2d198e51ce87baece338716d114b458de02d65c9ff808ca5b5b73723b4d1e962d9ac2d98176544dc9984cf8554d07820ef3dd0861cfe57b478328046380de589adad94ee44743ffac73bb7361feca5d56f07cf8ce75080e261282ae30350d7882679b15cab9e7e53ddf93310b33f7390ae5d318bb53f387e6af5d0ef4f947fc9cb8e7e38b52c7f8d772ece6156b38d88796ea19df02c53723b44df7c76315a0de9462f27287e682d2b4cda1a68fe00d7e48c51ee981be44e1ca940fb5190c12655edb4a83c3a4f33e48a015692df4f0b3d61656e362aca657b5ae8c12db5a0db3db1e45135ee918b66918f40e53c4f83e9da0cddfe63f736ae751ab3837a30ae3220d8e8e311487093a7b90c7e7e40dd54ca750e19452f9193aa892aa6a6229ab493dadae988b1724f7898ee69c36d3eb7364c4adbeca811cfe2065873e78c2b6dfdf1595f7a7831c07e03cda82e4f86f76438dfb2b07c13638ce7b509cfa71b88b5102b39a203b423202088e1c2103319cb32c13c1e546ff8612fa194c95a7808ab767c265a1bd5fa0efed5c8ec1701876a00ec8",
		"21001ecb6817f2805d044fd700100000dbd03029",
		"410277ef0b1fb1f60000000000040000c233000000080000000000000000",
	}
	for _, pkt := range packets {
		data, err := hex.DecodeString(pkt)
		if err != nil {
			t.Fatal(err)
		}
		if !IsUTP(data) {
			t.Errorf("expected uTP detection for packet len=%d", len(data))
		}
	}
}

func TestIsUTP_Invalid(t *testing.T) {
	t.Parallel()
	data, _ := hex.DecodeString("0102736470696e674958d580121500000000000079aaed6717a39c27b07c0c")
	if IsUTP(data) {
		t.Error("invalid uTP packet should not be detected")
	}
}

func TestIsUTP_TooShort(t *testing.T) {
	t.Parallel()
	if IsUTP([]byte{0x01, 0x00}) {
		t.Error("short packet should not be detected as uTP")
	}
}

func TestIsUDPTracker(t *testing.T) {
	t.Parallel()
	packets := []string{
		"00000417271019800000000078e90560",
		"00000417271019800000000022c5d64d",
		"000004172710198000000000b3863541",
	}
	for _, pkt := range packets {
		data, err := hex.DecodeString(pkt)
		if err != nil {
			t.Fatal(err)
		}
		if !IsUDPTracker(data) {
			t.Errorf("expected UDP tracker detection for packet %s", pkt)
		}
	}
}

func TestIsUDPTracker_TooShort(t *testing.T) {
	t.Parallel()
	if IsUDPTracker([]byte{0x00, 0x00, 0x04}) {
		t.Error("short packet should not be detected as UDP tracker")
	}
}

func TestIsUDPTracker_WrongMagic(t *testing.T) {
	t.Parallel()
	data, _ := hex.DecodeString("00000417271019810000000078e90560")
	if IsUDPTracker(data) {
		t.Error("wrong magic should not be detected")
	}
}
