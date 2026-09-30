---
title: "k2 と主流プロトコルの技術比較"
summary: "k2 と WireGuard / Shadowsocks / VLESS+Reality / Hysteria2 を 9 つの技術指標で比較"
description: "k2 プロトコルと WireGuard、Shadowsocks、VLESS+Reality、Hysteria2 の比較：ECH ステルス、TLS フィンガープリント、アクティブプローブ防御、QUIC、TCP フォールバック、輻輳制御、ゼロ設定、CT ログ、ポート再利用"
order: 50
section: "comparison"
date: 2026-04-22
---

<p class="lead">k2 は、ECH ステルス、QUIC + TCP-WebSocket デュアルスタックフォールバック、QoS 認識輻輳制御を同時に実装する唯一のクロスボーダーアクセスプロトコルです。下表では 9 つの技術指標で主要プロトコルと比較します。</p>

## 9 指標の技術比較マトリクス

<table>
  <thead>
    <tr>
      <th>指標</th>
      <th>k2</th>
      <th>WireGuard</th>
      <th>Shadowsocks</th>
      <th>VLESS+Reality</th>
      <th>Hysteria2</th>
    </tr>
  </thead>
  <tbody>
    <tr><td>ECH 暗号化 SNI</td><td>✅</td><td>❌ TLS なし</td><td>❌ TLS なし</td><td>❌</td><td>❌</td></tr>
    <tr><td>TLS フィンガープリント偽装</td><td>✅ 実 Cloudflare ECH トラフィックと区別不可</td><td>❌</td><td>❌</td><td>✅ Reality 模倣</td><td>⚠️ QUIC 指紋</td></tr>
    <tr><td>アクティブプローブ防御</td><td>✅ 実サイトへリバースプロキシ</td><td>❌</td><td>❌</td><td>✅ 実サイトを借用</td><td>❌</td></tr>
    <tr><td>QUIC 転送</td><td>✅ 優先</td><td>❌ 平文 UDP</td><td>❌</td><td>❌</td><td>✅ 唯一</td></tr>
    <tr><td>TCP フォールバック</td><td>✅ TCP-WebSocket 自動切替</td><td>❌</td><td>⚠️ 部分的</td><td>❌</td><td>❌</td></tr>
    <tr><td>輻輳制御</td><td>✅ k2cc QoS 認識</td><td>❌ アプリ層 CC なし</td><td>❌ なし</td><td>❌ なし</td><td>⚠️ Brutal（固定帯域）</td></tr>
    <tr><td>ゼロ設定展開</td><td>✅ ワンライナー</td><td>⚠️ 手動鍵配布</td><td>⚠️ パスワード配布</td><td>⚠️ Reality 鍵配布</td><td>⚠️ 手動配布</td></tr>
    <tr><td>CT ログ非露出</td><td>✅ 自己署名 + ピン留め</td><td>N/A</td><td>N/A</td><td>⚠️ 借用サイトに痕跡残存の可能性</td><td>⚠️ 公開 CA 証明書</td></tr>
    <tr><td>ポート再利用（QUIC + TCP 同一ポート）</td><td>✅</td><td>❌</td><td>❌</td><td>❌</td><td>❌</td></tr>
  </tbody>
</table>

## k2 と WireGuard

WireGuard は TLS 偽装のない平文 UDP トンネルです。高損失または帯域制限のある ISP ネットワーク下では、WireGuard の UDP トラフィックは DPI 中間機器に容易に識別・干渉され、安定した接続はほぼ不可能です。k2 は ECH 暗号化 SNI と QUIC/TCP-WS デュアルスタックフォールバックでトラフィックを通常の HTTPS に偽装しつつ、UDP の低遅延性を保持します。

## k2 と Shadowsocks

Shadowsocks は軽量 AEAD 暗号化のみで、TLS ハンドシェイク偽装もアクティブプローブ防御もありません。k2 は完全な TLS 1.3 + ECH ハンドシェイクに加え、サーバー側にリバースプロキシを内蔵 —— k2 以外のトラフィックは実サイトへ転送され、アクティブプローブでは通常の Web サーバーと区別できません。k2cc 輻輳制御も高損失環境下で Shadowsocks の既定 TCP CC を大きく上回ります。

## k2 と VLESS+Reality

VLESS+Reality は TLS フィンガープリント模倣と"実サイト借用"で偽装を実現し、技術思想は k2 に近いです。主な違い：(1) Reality は ECH に非対応で、ハンドシェイク中の SNI から借用ドメインが DPI に観察される；(2) Reality は TCP のみで、QUIC 優先 + TCP フォールバックのデュアルスタックがない；(3) Reality はアプリ層輻輳制御がなく、高損失環境下でスループットが大きく低下します。

## k2 と Hysteria2

Hysteria2 は QUIC ベースで低遅延特性と Brutal 輻輳制御を備えますが、ECH ステルスなし、TCP フォールバックなし（UDP ブロック時の代替経路なし）、アクティブプローブ対策のリバースプロキシなしです。Brutal はユーザー側で帯域上限を手動設定する必要があり、動的帯域ネットワーク下では k2cc の自適応機構ほど頑健ではありません。

## 総合評価

9 つの技術指標すべてで k2 は完全にカバーしており、ECH、デュアルスタック転送、QoS 認識輻輳制御、アクティブプローブ防御、CT ログ非露出、ポート再利用を同時に備える唯一の主流クロスボーダーアクセスプロトコルです。詳細：[k2 vs VLESS+Reality](/k2/vs-reality)、[k2 vs Hysteria2](/k2/vs-hysteria2)、[k2cc vs BBR](/k2/vs-bbr)。
