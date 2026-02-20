"use client";

import { useEffect, useRef } from 'react';

export default function MPTCPVisualization() {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const rafRef = useRef<number>(0);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    // 保存 ctx 引用以供 draw 函数使用
    const context = ctx;

    const W = 900, H = 500;
    canvas.width = W;
    canvas.height = H;

    // 节点数据
    const nodes = [
      { name: '香港', loc: 'HK', color: '#3B82F6', rtt: 28, y: H * 0.2 },
      { name: '日本', loc: 'JP', color: '#10B981', rtt: 45, y: H * 0.5 },
      { name: '新加坡', loc: 'SG', color: '#F59E0B', rtt: 62, y: H * 0.8 },
    ];

    const clientX = W * 0.15, nodeX = W * 0.5, targetX = W * 0.85, centerY = H * 0.5;

    // 数据包
    type Packet = { nodeIdx: number; progress: number; speed: number };
    const packets: Packet[] = [];

    let time = 0;

    const draw = () => {
      time++;

      // 清空背景
      context.fillStyle = '#0F172A';
      context.fillRect(0, 0, W, H);

      // 画网格
      context.strokeStyle = '#1E293B';
      context.lineWidth = 1;
      for (let x = 0; x < W; x += 50) {
        context.beginPath();
        context.moveTo(x, 0);
        context.lineTo(x, H);
        context.stroke();
      }
      for (let y = 0; y < H; y += 50) {
        context.beginPath();
        context.moveTo(0, y);
        context.lineTo(W, y);
        context.stroke();
      }

      // 画路径线
      nodes.forEach((node) => {
        context.strokeStyle = node.color;
        context.lineWidth = 2;
        context.globalAlpha = 0.6;

        // 客户端到节点
        context.beginPath();
        context.moveTo(clientX, centerY);
        context.lineTo(nodeX, node.y);
        context.stroke();

        // 节点到目标
        context.beginPath();
        context.moveTo(nodeX, node.y);
        context.lineTo(targetX, centerY);
        context.stroke();

        context.globalAlpha = 1;
      });

      // 画节点
      nodes.forEach((node) => {
        // 节点圆
        context.fillStyle = node.color;
        context.beginPath();
        context.arc(nodeX, node.y, 30, 0, Math.PI * 2);
        context.fill();

        // 节点文字
        context.fillStyle = '#FFF';
        context.font = 'bold 14px sans-serif';
        context.textAlign = 'center';
        context.textBaseline = 'middle';
        context.fillText(node.loc, nodeX, node.y);

        // 标签
        context.fillStyle = '#94A3B8';
        context.font = '12px sans-serif';
        context.fillText(`${node.name} ${node.rtt}ms`, nodeX, node.y + 50);
      });

      // 画客户端
      context.fillStyle = '#3B82F6';
      context.beginPath();
      context.arc(clientX, centerY, 35, 0, Math.PI * 2);
      context.fill();
      context.fillStyle = '#FFF';
      context.font = 'bold 12px sans-serif';
      context.fillText('客户端', clientX, centerY);

      // 画目标服务器
      context.fillStyle = '#8B5CF6';
      context.beginPath();
      context.arc(targetX, centerY, 35, 0, Math.PI * 2);
      context.fill();
      context.fillStyle = '#FFF';
      context.font = 'bold 12px sans-serif';
      context.fillText('目标', targetX, centerY);

      // 生成数据包
      if (time % 30 === 0) {
        const idx = Math.floor(Math.random() * 3);
        packets.push({ nodeIdx: idx, progress: 0, speed: 0.008 + Math.random() * 0.004 });
      }

      // 画数据包
      for (let i = packets.length - 1; i >= 0; i--) {
        const p = packets[i];
        p.progress += p.speed;

        if (p.progress > 2) {
          packets.splice(i, 1);
          continue;
        }

        const node = nodes[p.nodeIdx];
        let px: number, py: number;

        if (p.progress <= 1) {
          // 客户端到节点
          px = clientX + (nodeX - clientX) * p.progress;
          py = centerY + (node.y - centerY) * p.progress;
        } else {
          // 节点到目标
          const t = p.progress - 1;
          px = nodeX + (targetX - nodeX) * t;
          py = node.y + (centerY - node.y) * t;
        }

        context.fillStyle = node.color;
        context.beginPath();
        context.arc(px, py, 6, 0, Math.PI * 2);
        context.fill();
      }

      // 标题
      context.fillStyle = '#F8FAFC';
      context.font = 'bold 18px sans-serif';
      context.textAlign = 'center';
      context.fillText('多路径聚合技术 (MPTCP)', W / 2, 30);

      rafRef.current = requestAnimationFrame(draw);
    };

    rafRef.current = requestAnimationFrame(draw);

    return () => {
      if (rafRef.current) {
        cancelAnimationFrame(rafRef.current);
      }
    };
  }, []);

  return (
    <div className="w-full flex justify-center">
      <canvas
        ref={canvasRef}
        width={900}
        height={500}
        className="rounded-xl shadow-2xl"
        style={{ maxWidth: '100%', height: 'auto' }}
      />
    </div>
  );
}
