// forge web/fingerprint browser probe. Reads window.__fp = {token, url, canvas, webgl}
// set by the page, collects a minimal high-signal payload, and POSTs it once.
(function () {
  var cfg = window.__fp || {};
  if (!cfg.token || !cfg.url) return;
  var d = {
    timezone: (Intl.DateTimeFormat().resolvedOptions().timeZone) || "",
    languages: (navigator.languages || []).slice(0, 10),
    platform: navigator.platform || "",
    hardwareConcurrency: navigator.hardwareConcurrency || 0,
    webdriver: navigator.webdriver === true
  };
  if (cfg.canvas) {
    try {
      var c = document.createElement("canvas");
      var g = c.getContext("2d");
      g.textBaseline = "top";
      g.font = "14px 'Arial'";
      g.fillText("forge-fp", 2, 2);
      d.canvas = c.toDataURL().slice(-64);
    } catch (e) {}
  }
  if (cfg.webgl) {
    try {
      var gl = document.createElement("canvas").getContext("webgl");
      var ext = gl.getExtension("WEBGL_debug_renderer_info");
      d.webgl = ext ? String(gl.getParameter(ext.UNMASKED_RENDERER_WEBGL)).slice(0, 64) : "";
    } catch (e) {}
  }
  try {
    fetch(cfg.url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token: cfg.token, data: d }),
      keepalive: true
    });
  } catch (e) {}
})();
