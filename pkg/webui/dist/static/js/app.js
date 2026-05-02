(() => {
  const { useEffect, useState } = React;
  const { createRoot } = ReactDOM;

  const tabs = ["status", "wifi", "history", "gallery", "config"];

  function Card({ title, children }) {
    return React.createElement("section", { className: "card" },
      React.createElement("h2", null, title),
      children
    );
  }

  function Loading() {
    return React.createElement("p", { style: { color: "#a5b6ff" } }, "Loading data from device...");
  }

  function StatusPanel() {
    const [status, setStatus] = useState(null);
    const [error, setError] = useState("");

    const load = () => {
      setError("");
      fetch("/api/status")
        .then((resp) => resp.json())
        .then(setStatus)
        .catch((e) => setError(String(e.message || e)));
    };

    useEffect(() => {
      load();
    }, []);

    return React.createElement(Card, { title: "Status" },
      React.createElement("div", { className: "row" },
        React.createElement("div", { className: "stat" }, React.createElement("h3", null, "State"), React.createElement("pre", null, error || (status ? JSON.stringify(status, null, 2) : ""))),
        React.createElement("div", { className: "stat" }, React.createElement("h3", null, "Actions"),
          React.createElement("button", { onClick: load }, "Refresh status")
        )
      ),
      !status && !error ? React.createElement(Loading, null) : null
    );
  }

  function WifiPanel() {
    const [networks, setNetworks] = useState([]);
    const [error, setError] = useState("");

    const load = () => {
      setError("");
      fetch("/api/wifi/networks")
        .then((r) => r.json())
        .then((data) => setNetworks(Array.isArray(data.networks) ? data.networks : []))
        .catch((e) => setError(String(e.message || e)));
    };

    useEffect(load, []);

    return React.createElement(Card, { title: "Wi-Fi" },
      React.createElement("div", { className: "row" },
        React.createElement("div", { className: "stat" },
          React.createElement("h3", null, "Saved Networks"),
          React.createElement("pre", null, JSON.stringify(networks, null, 2) || "[]")
        ),
        React.createElement("div", { className: "stat" },
          React.createElement("h3", null, "Discovery"),
          React.createElement("p", null, "Run /api/wifi/scan from API when scan mode is available on device firmware."),
          error ? React.createElement("p", null, error) : null,
          React.createElement("button", { onClick: load }, "Reload")
        )
      )
    );
  }

  function Placeholder({ title }) {
    return React.createElement(Card, { title }, React.createElement("p", { style: { color: '#9fb1ff' } }, "UI still migrating to React; backend APIs remain compatible."));
  }

  function App() {
    const [tab, setTab] = useState("status");

    useEffect(() => {
      const hash = window.location.hash.replace(/^#\//, "");
      if (tabs.includes(hash)) setTab(hash);
      const onHash = () => {
        const next = window.location.hash.replace(/^#\//, "");
        if (tabs.includes(next)) setTab(next);
      };
      window.addEventListener("hashchange", onHash);
      return () => window.removeEventListener("hashchange", onHash);
    }, []);

    return React.createElement("div", { className: "app-shell" },
      React.createElement("h1", null, "Photo Backup Station"),
      React.createElement("p", { style: { color: '#a8b8ff' } }, "Single-page React client, API-first architecture."),
      React.createElement("div", { className: "nav" },
        tabs.map((item) =>
          React.createElement("button", {
            key: item,
            className: tab === item ? "active" : "",
            onClick: () => { setTab(item); window.location.hash = `#/${item}`; }
          }, item.toUpperCase())
        )
      ),
      tab === "status" && React.createElement(StatusPanel, null),
      tab === "wifi" && React.createElement(WifiPanel, null),
      tab === "history" && React.createElement(Placeholder, { title: "History" }),
      tab === "gallery" && React.createElement(Placeholder, { title: "Gallery" }),
      tab === "config" && React.createElement(Placeholder, { title: "Config" })
    );
  }

  createRoot(document.getElementById("root")).render(React.createElement(App));
})();
