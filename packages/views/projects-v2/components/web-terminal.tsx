"use client";

import { useEffect, useRef, useCallback } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";

interface WebTerminalProps {
  sessionName: string;
  className?: string;
}

export function WebTerminal({ sessionName, className }: WebTerminalProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const fitRef = useRef<FitAddon | null>(null);

  const connect = useCallback(() => {
    if (!containerRef.current) return;

    const term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: '"Cascadia Code", "Fira Code", "JetBrains Mono", monospace',
      theme: {
        background: "#0a0a0a",
        foreground: "#e4e4e7",
        cursor: "#e4e4e7",
        selectionBackground: "#27272a",
      },
      allowProposedApi: true,
    });

    const fitAddon = new FitAddon();
    const webLinksAddon = new WebLinksAddon();
    term.loadAddon(fitAddon);
    term.loadAddon(webLinksAddon);
    term.open(containerRef.current);
    fitAddon.fit();

    termRef.current = term;
    fitRef.current = fitAddon;

    // Connect WebSocket
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const wsUrl = `${protocol}//${window.location.host}/api/terminal/${sessionName}`;
    const ws = new WebSocket(wsUrl);
    ws.binaryType = "arraybuffer";
    wsRef.current = ws;

    const sendResize = () => {
      if (ws.readyState !== WebSocket.OPEN) return;
      const payload = JSON.stringify({ cols: term.cols, rows: term.rows });
      const encoded = new TextEncoder().encode(payload);
      const msg = new Uint8Array(1 + encoded.length);
      msg[0] = 1;
      msg.set(encoded, 1);
      ws.send(msg);
    };

    ws.onopen = () => {
      sendResize();
    };

    ws.onmessage = (event) => {
      const data =
        event.data instanceof ArrayBuffer
          ? new Uint8Array(event.data)
          : event.data;
      term.write(data);
    };

    ws.onclose = () => {
      term.write("\r\n\x1b[31m[Connection closed]\x1b[0m\r\n");
    };

    // Terminal input → WebSocket
    term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(new TextEncoder().encode(data));
      }
    });

    // Handle resize
    term.onResize(() => {
      sendResize();
    });

    const handleResize = () => {
      fitAddon.fit();
    };

    const resizeObserver = new ResizeObserver(handleResize);
    resizeObserver.observe(containerRef.current);

    return () => {
      resizeObserver.disconnect();
      ws.close();
      term.dispose();
    };
  }, [sessionName]);

  useEffect(() => {
    const cleanup = connect();
    return cleanup;
  }, [connect]);

  return (
    <div
      ref={containerRef}
      className={`h-full w-full rounded-lg overflow-hidden bg-[#0a0a0a] ${className ?? ""}`}
    />
  );
}
