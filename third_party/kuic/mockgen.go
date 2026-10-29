//go:build gomock || generate

package quic

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/kaitu-io/kuic -destination mock_send_conn_test.go github.com/kaitu-io/kuic SendConn"
type SendConn = sendConn

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/kaitu-io/kuic -destination mock_raw_conn_test.go github.com/kaitu-io/kuic RawConn"
type RawConn = rawConn

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/kaitu-io/kuic -destination mock_sender_test.go github.com/kaitu-io/kuic Sender"
type Sender = sender

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/kaitu-io/kuic -destination mock_stream_sender_test.go github.com/kaitu-io/kuic StreamSender"
type StreamSender = streamSender

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/kaitu-io/kuic -destination mock_stream_control_frame_getter_test.go github.com/kaitu-io/kuic StreamControlFrameGetter"
type StreamControlFrameGetter = streamControlFrameGetter

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/kaitu-io/kuic -destination mock_stream_frame_getter_test.go github.com/kaitu-io/kuic StreamFrameGetter"
type StreamFrameGetter = streamFrameGetter

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/kaitu-io/kuic -destination mock_frame_source_test.go github.com/kaitu-io/kuic FrameSource"
type FrameSource = frameSource

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/kaitu-io/kuic -destination mock_ack_frame_source_test.go github.com/kaitu-io/kuic AckFrameSource"
type AckFrameSource = ackFrameSource

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/kaitu-io/kuic -destination mock_sealing_manager_test.go github.com/kaitu-io/kuic SealingManager"
type SealingManager = sealingManager

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/kaitu-io/kuic -destination mock_unpacker_test.go github.com/kaitu-io/kuic Unpacker"
type Unpacker = unpacker

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/kaitu-io/kuic -destination mock_packer_test.go github.com/kaitu-io/kuic Packer"
type Packer = packer

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/kaitu-io/kuic -destination mock_mtu_discoverer_test.go github.com/kaitu-io/kuic MTUDiscoverer"
type MTUDiscoverer = mtuDiscoverer

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/kaitu-io/kuic -destination mock_conn_runner_test.go github.com/kaitu-io/kuic ConnRunner"
type ConnRunner = connRunner

//go:generate sh -c "go tool mockgen -typed -build_flags=\"-tags=gomock\" -package quic -self_package github.com/kaitu-io/kuic -destination mock_packet_handler_test.go github.com/kaitu-io/kuic PacketHandler"
type PacketHandler = packetHandler

//go:generate sh -c "go tool mockgen -typed -package quic -self_package github.com/kaitu-io/kuic -self_package github.com/kaitu-io/kuic -destination mock_packetconn_test.go net PacketConn"
