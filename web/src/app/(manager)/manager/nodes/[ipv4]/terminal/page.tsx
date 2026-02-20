"use client";

import { useEffect, useRef, useState, useCallback } from "react";
import { useParams } from "next/navigation";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { api } from "@/lib/api";

interface TerminalMessage {
  type: "input" | "output" | "resize" | "error";
  data?: string;
  cols?: number;
  rows?: number;
}

type ConnectionStatus = "connecting" | "connected" | "disconnected" | "error";

export default function SSHTerminalPage() {
  const params = useParams();
  const nodeIPv4 = params.ipv4 as string;

  const termRef = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);

  const [status, setStatus] = useState<ConnectionStatus>("connecting");
  const [errorMessage, setErrorMessage] = useState<string>("");

  const connect = useCallback(async () => {
    if (!termRef.current || !nodeIPv4) return;

    // Initialize terminal
    const term = new Terminal({
      cursorBlink: true,
      fontSize: 14,
      fontFamily: 'Menlo, Monaco, "Courier New", monospace',
      theme: {
        background: "#1e1e1e",
        foreground: "#d4d4d4",
        cursor: "#d4d4d4",
        cursorAccent: "#1e1e1e",
        selectionBackground: "#264f78",
      },
    });

    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(termRef.current);
    fitAddon.fit();

    terminalRef.current = term;
    fitAddonRef.current = fitAddon;

    term.writeln(`Connecting to ${nodeIPv4}...`);
    setStatus("connecting");

    // Get WebSocket authentication token
    // WebSocket connections to k2.52j.me cannot use cookies from www.kaitu.io (cross-domain)
    // So we get a short-lived token and pass it as a URL query parameter
    let wsToken: string;
    try {
      term.writeln("Authenticating...");
      const tokenResponse = await api.getWsToken();
      wsToken = tokenResponse.token;
    } catch (err) {
      const errMsg = err instanceof Error ? err.message : "Authentication failed";
      setStatus("error");
      setErrorMessage(errMsg);
      term.writeln(`\r\n\x1b[31mAuthentication failed: ${errMsg}\x1b[0m`);
      return;
    }

    // Build WebSocket URL with token - connect directly to backend
    // Amplify/Next.js rewrites don't support WebSocket proxy, so we connect directly
    const wsUrl = `wss://k2.52j.me/app/nodes/${encodeURIComponent(nodeIPv4)}/terminal?token=${encodeURIComponent(wsToken)}`;

    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    ws.onopen = () => {
      setStatus("connected");
      term.clear();
    };

    ws.onmessage = (event) => {
      try {
        const msg: TerminalMessage = JSON.parse(event.data);
        if (msg.type === "output" && msg.data) {
          term.write(msg.data);
        } else if (msg.type === "error" && msg.data) {
          setStatus("error");
          setErrorMessage(msg.data);
          term.writeln(`\r\n\x1b[31mError: ${msg.data}\x1b[0m`);
        }
      } catch {
        // If not JSON, treat as raw output
        term.write(event.data);
      }
    };

    ws.onclose = () => {
      setStatus("disconnected");
      term.writeln("\r\n\x1b[33mConnection closed.\x1b[0m");
    };

    ws.onerror = () => {
      setStatus("error");
      setErrorMessage("WebSocket connection failed");
      term.writeln("\r\n\x1b[31mConnection error.\x1b[0m");
    };

    // Send terminal input to WebSocket
    term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        const msg: TerminalMessage = { type: "input", data };
        ws.send(JSON.stringify(msg));
      }
    });

    // Handle terminal resize
    term.onResize(({ cols, rows }) => {
      if (ws.readyState === WebSocket.OPEN) {
        const msg: TerminalMessage = { type: "resize", cols, rows };
        ws.send(JSON.stringify(msg));
      }
    });

    // Handle window resize
    const handleResize = () => {
      if (fitAddonRef.current) {
        fitAddonRef.current.fit();
      }
    };
    window.addEventListener("resize", handleResize);

    return () => {
      window.removeEventListener("resize", handleResize);
      ws.close();
      term.dispose();
    };
  }, [nodeIPv4]);

  useEffect(() => {
    let cleanup: (() => void) | undefined;

    const initConnection = async () => {
      cleanup = await connect();
    };

    initConnection();

    return () => {
      cleanup?.();
    };
  }, [connect]);

  const handleReconnect = async () => {
    // Clean up existing connections
    if (wsRef.current) {
      wsRef.current.close();
    }
    if (terminalRef.current) {
      terminalRef.current.dispose();
    }
    setErrorMessage("");
    // Reconnect
    await connect();
  };

  return (
    <div className="h-screen flex flex-col bg-[#1e1e1e]">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-2 bg-[#2d2d2d] border-b border-[#404040]">
        <div className="flex items-center gap-3">
          <h1 className="text-sm font-medium text-gray-200">
            SSH Terminal - {nodeIPv4}
          </h1>
          <span
            className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${
              status === "connected"
                ? "bg-green-900 text-green-200"
                : status === "connecting"
                ? "bg-yellow-900 text-yellow-200"
                : status === "error"
                ? "bg-red-900 text-red-200"
                : "bg-gray-700 text-gray-300"
            }`}
          >
            {status === "connected"
              ? "Connected"
              : status === "connecting"
              ? "Connecting..."
              : status === "error"
              ? "Error"
              : "Disconnected"}
          </span>
        </div>
        <div className="flex items-center gap-2">
          {(status === "disconnected" || status === "error") && (
            <button
              onClick={handleReconnect}
              className="px-3 py-1 text-xs font-medium text-white bg-blue-600 rounded hover:bg-blue-700 transition-colors"
            >
              Reconnect
            </button>
          )}
        </div>
      </div>

      {/* Error message */}
      {errorMessage && (
        <div className="px-4 py-2 bg-red-900/50 text-red-200 text-sm">
          {errorMessage}
        </div>
      )}

      {/* Terminal container */}
      <div className="flex-1 p-2">
        <div ref={termRef} className="h-full w-full" />
      </div>
    </div>
  );
}
