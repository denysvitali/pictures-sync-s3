(() => {
  const { useEffect, useMemo, useRef, useState } = React;
  const { createRoot } = ReactDOM;

  const TABS = ["status", "wifi", "history", "gallery", "config"];
  const DEFAULT_TAB = "status";
  const LS_DEVICE_URL = "pbs-device-url";

  function useDeviceUrl() {
    const [deviceUrl, setDeviceUrlState] = useState(() => {
      const queryUrl = new URLSearchParams(window.location.search).get("device") ||
        new URLSearchParams(window.location.search).get("url");
      const saved = localStorage.getItem(LS_DEVICE_URL);
      const initial = queryUrl || saved || `${window.location.origin}`;
      return normalizeBaseUrl(initial);
    });

    const setDeviceUrl = (next) => {
      const normalized = normalizeBaseUrl(next);
      setDeviceUrlState(normalized);
      localStorage.setItem(LS_DEVICE_URL, normalized);
    };

    return [deviceUrl, setDeviceUrl];
  }

  function normalizeBaseUrl(value) {
    const trimmed = String(value || "").trim();
    if (!trimmed) {
      return "";
    }
    try {
      const candidate = trimmed.match(/^https?:\/\//i) ? trimmed : `https://${trimmed}`;
      const parsed = new URL(candidate);
      if ((parsed.protocol !== "https:" && parsed.protocol !== "http:") || !parsed.host) {
        return "";
      }
      return `${parsed.protocol}//${parsed.host}`;
    } catch {
      return "";
    }
  }

  function apiUrl(base, path) {
    return `${base}${path}`;
  }

  function wsUrl(base) {
    if (!base) {
      return "";
    }
    const protocol = base.startsWith("https://") ? "wss://" : "ws://";
    return `${protocol}${base.replace(/^https?:\/\//, "")}/ws`;
  }

  function withCredentialsHeaders() {
    return { credentials: "include" };
  }

  async function toJson(response) {
    if (response.status === 401 || response.status === 403) {
      const text = await response.text();
      throw new Error(`Auth required (${response.status}). Open dev tools and provide credentials.`);
    }
    if (!response.ok) {
      const text = await response.text();
      throw new Error(text || `Request failed (${response.status})`);
    }
    const type = response.headers.get("content-type") || "";
    if (!type.includes("application/json")) {
      return response.text();
    }
    return response.json();
  }

  function Card({ title, children }) {
    return React.createElement("section", { className: "card" },
      React.createElement("h2", null, title),
      children
    );
  }

  function DeviceSwitcher({ deviceUrl, onChange }) {
    const [value, setValue] = useState(deviceUrl);
    const onSave = (ev) => {
      ev.preventDefault();
      onChange(value);
    };

    return React.createElement("form", { className: "control-card", onSubmit: onSave },
      React.createElement("label", { htmlFor: "device-input" }, "Device base URL"),
      React.createElement("div", { className: "row" },
        React.createElement("input", {
          id: "device-input",
          value: value,
          onChange: (event) => setValue(event.target.value),
          placeholder: "http://192.168.1.10:8080",
          spellCheck: false
        }),
        React.createElement("button", { type: "submit" }, "Use device")
      ),
      React.createElement("p", { className: "hint" },
        "If you are opening this page from GitHub Pages, this must point at your device address and include protocol/port where needed."
      )
    );
  }

  function JsonBlock({ value }) {
    return React.createElement("pre", null, JSON.stringify(value, null, 2));
  }

  function LoadingState({ status }) {
    return React.createElement("p", { className: "loading" }, status || "Loading...");
  }

  function ErrorState({ error }) {
    return React.createElement("div", { className: "error" },
      React.createElement("p", null, String(error || "Unknown error"))
    );
  }

  function StatusPanel({ deviceUrl }) {
    const [status, setStatus] = useState(null);
    const [history, setHistory] = useState([]);
    const [error, setError] = useState("");
    const [loading, setLoading] = useState(false);

    const load = async () => {
      if (!deviceUrl) {
        return;
      }
      setLoading(true);
      setError("");
      try {
        const [statusResponse, historyResponse] = await Promise.all([
          fetch(apiUrl(deviceUrl, "/api/status"), { ...withCredentialsHeaders() }).then(toJson),
          fetch(apiUrl(deviceUrl, "/api/history"), { ...withCredentialsHeaders() }).then(toJson)
        ]);
        setStatus(statusResponse);
        setHistory((historyResponse && historyResponse.history) || []);
      } catch (err) {
        setError(err.message || String(err));
      } finally {
        setLoading(false);
      }
    };

    useEffect(() => {
      load();
    }, [deviceUrl]);

    return React.createElement(Card, { title: "Status" },
      loading && React.createElement(LoadingState, { status: "Loading status..." }),
      error && React.createElement(ErrorState, { error }),
      React.createElement("div", { className: "row" },
        React.createElement("div", { className: "stat" },
          React.createElement("h3", null, "Current status"),
          React.createElement("pre", null, status ? JSON.stringify(status, null, 2) : "{}")
        ),
        React.createElement("div", { className: "stat" },
          React.createElement("h3", null, "Recent history"),
          React.createElement("p", null, `Items: ${Array.isArray(history) ? history.length : 0}`),
          React.createElement(JsonBlock, { value: history })
        )
      ),
      React.createElement("div", { className: "actions" },
        React.createElement("button", { onClick: load }, "Refresh")
      )
    );
  }

  function WifiPanel({ deviceUrl }) {
    const [state, setState] = useState({});
    const [networks, setNetworks] = useState([]);
    const [error, setError] = useState("");
    const [loading, setLoading] = useState(false);

    const refresh = async () => {
      if (!deviceUrl) {
        return;
      }
      setLoading(true);
      setError("");
      try {
        const [status, networksResult] = await Promise.all([
          fetch(apiUrl(deviceUrl, "/api/wifi/status"), withCredentialsHeaders()).then(toJson),
          fetch(apiUrl(deviceUrl, "/api/wifi/networks"), withCredentialsHeaders()).then(toJson)
        ]);
        setState(status);
        setNetworks(Array.isArray(networksResult.networks) ? networksResult.networks : []);
      } catch (err) {
        setError(err.message || String(err));
      } finally {
        setLoading(false);
      }
    };

    useEffect(() => {
      refresh();
    }, [deviceUrl]);

    return React.createElement(Card, { title: "Wi-Fi" },
      loading && React.createElement(LoadingState, { status: "Loading Wi-Fi info..." }),
      error && React.createElement(ErrorState, { error }),
      React.createElement("div", { className: "row" },
        React.createElement("div", { className: "stat" },
          React.createElement("h3", null, "Wi-Fi status"),
          React.createElement(JsonBlock, { value: state })
        ),
        React.createElement("div", { className: "stat" },
          React.createElement("h3", null, "Saved networks"),
          React.createElement(JsonBlock, { value: networks })
        )
      ),
      React.createElement("div", { className: "actions" },
        React.createElement("button", { onClick: refresh }, "Refresh")
      )
    );
  }

  function PlaceholderPanel({ title }) {
    return React.createElement(Card, { title },
      React.createElement("p", null, `${title} is available in the API and will be completed in the next iteration.`),
      React.createElement("p", { className: "muted" }, "The backend endpoint is already exposed and wired from the on-device service.")
    );
  }

  function useDeviceConnection(deviceUrl) {
    const [error, setError] = useState("");
    const [connected, setConnected] = useState(false);
    const wsRef = useRef(null);

    useEffect(() => {
      if (!deviceUrl) {
        setConnected(false);
        return;
      }

      let cleanup = false;
      const connect = async () => {
        try {
          const tokenResponse = await fetch(apiUrl(deviceUrl, "/api/ws-token"), {
            ...withCredentialsHeaders()
          });
          const tokenPayload = await toJson(tokenResponse);
          if (!tokenPayload || !tokenPayload.ws_token) {
            throw new Error("WebSocket token missing from response");
          }

          const socket = new WebSocket(wsUrl(deviceUrl));
          wsRef.current = socket;
          socket.onopen = () => {
            setConnected(true);
            setError("");
            try {
              socket.send(JSON.stringify({ type: "auth", token: tokenPayload.ws_token }));
            } catch {
              socket.close();
            }
          };
          socket.onclose = () => setConnected(false);
          socket.onerror = () => setError("WebSocket connection failed");
          socket.onmessage = (event) => {
            try {
              const payload = JSON.parse(event.data);
              console.debug("ws", payload);
            } catch {
              console.debug("ws", event.data);
            }
          };
        } catch (err) {
          if (!cleanup) {
            setConnected(false);
            setError(err.message || String(err));
          }
        }
      };

      connect();

      return () => {
        cleanup = true;
        if (wsRef.current && wsRef.current.readyState <= 1) {
          wsRef.current.close();
        }
      };
    }, [deviceUrl]);

    return { connected, error };
  }

  function AppShell() {
    const [deviceUrl, setDeviceUrl] = useDeviceUrl();
    const [tab, setTab] = useState(DEFAULT_TAB);

    const connection = useDeviceConnection(deviceUrl);

    const activeTab = useMemo(() => {
      return TABS.includes(tab) ? tab : DEFAULT_TAB;
    }, [tab]);

    useEffect(() => {
      const onHash = () => {
        const raw = window.location.hash.replace(/^#\//, "").trim();
        if (TABS.includes(raw)) {
          setTab(raw);
        }
      };

      onHash();
      window.addEventListener("hashchange", onHash);
      return () => window.removeEventListener("hashchange", onHash);
    }, []);

    const onSelectTab = (next) => {
      setTab(next);
      window.location.hash = `#/${next}`;
    };

    return React.createElement("div", { className: "app-shell" },
      React.createElement("h1", null, "Photo Backup Station"),
      React.createElement("p", { className: "lead" }, "API-first React UI with local-device and GitHub Pages support"),
      React.createElement("div", { className: connection.connected ? "connection-state ok" : "connection-state" },
        React.createElement("span", null, connection.connected ? "Connected" : "Disconnected"),
        connection.error ? React.createElement("span", { className: "muted" }, ` · ${connection.error}`) : null
      ),
      React.createElement(DeviceSwitcher, {
        deviceUrl,
        onChange: (next) => {
          setDeviceUrl(next);
        }
      }),
      React.createElement("div", { className: "nav" },
        TABS.map((item) =>
          React.createElement("button", {
            key: item,
            className: item === activeTab ? "active" : "",
            onClick: () => onSelectTab(item)
          }, item.toUpperCase())
        )
      ),
      activeTab === "status" && React.createElement(StatusPanel, { deviceUrl }),
      activeTab === "wifi" && React.createElement(WifiPanel, { deviceUrl }),
      activeTab === "history" && React.createElement(PlaceholderPanel, { title: "History" }),
      activeTab === "gallery" && React.createElement(PlaceholderPanel, { title: "Gallery" }),
      activeTab === "config" && React.createElement(PlaceholderPanel, { title: "Config" })
    );
  }

  createRoot(document.getElementById("root")).render(React.createElement(AppShell));
})();
