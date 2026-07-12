// forge web/fingerprint browser probe. Reads window.__fp = {token, url, canvas,
// webgl, audio, fonts} set by the page, collects a device signal payload, and
// POSTs it once after any async collectors resolve.
(function () {
  var cfg = window.__fp || {};
  if (!cfg.token || !cfg.url) return;
  var s = window.screen || {};
  var d = {
    timezone: (Intl.DateTimeFormat().resolvedOptions().timeZone) || "",
    languages: (navigator.languages || []).slice(0, 10),
    platform: navigator.platform || "",
    hardwareConcurrency: navigator.hardwareConcurrency || 0,
    maxTouchPoints: navigator.maxTouchPoints || 0,
    deviceMemory: navigator.deviceMemory ? String(navigator.deviceMemory) : "",
    screen: (s.width || 0) + "x" + (s.height || 0) + "x" + (s.colorDepth || 0),
    devicePixelRatio: window.devicePixelRatio ? String(window.devicePixelRatio) : "",
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
      if (ext) {
        d.webgl = String(gl.getParameter(ext.UNMASKED_RENDERER_WEBGL)).slice(0, 64);
        d.webglVendor = String(gl.getParameter(ext.UNMASKED_VENDOR_WEBGL)).slice(0, 64);
      }
    } catch (e) {}
  }
  if (cfg.fonts) {
    try { d.fonts = detectFonts(); } catch (e) {}
  }

  var tasks = [];
  if (cfg.audio) {
    tasks.push(audioHash().then(function (h) { d.audio = h; }).catch(function () {}));
  }
  if (navigator.userAgentData && navigator.userAgentData.getHighEntropyValues) {
    tasks.push(navigator.userAgentData
      .getHighEntropyValues(["platform", "platformVersion", "model", "architecture", "bitness"])
      .then(function (h) {
        d.uadata = [h.platform, h.platformVersion, h.model, h.architecture, h.bitness].join("|").slice(0, 128);
      }).catch(function () {}));
  }

  function send() {
    try {
      fetch(cfg.url, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token: cfg.token, data: d }),
        keepalive: true
      });
    } catch (e) {}
  }

  // hash32 is a tiny FNV-1a string hash rendered as 8 hex chars. NOT
  // cryptographic — only a compact, stable identifier for a collected value.
  function hash32(str) {
    var h = 0x811c9dc5;
    for (var i = 0; i < str.length; i++) {
      h ^= str.charCodeAt(i);
      h = (h * 0x01000193) >>> 0;
    }
    return ("0000000" + h.toString(16)).slice(-8);
  }

  // detectFonts returns "hash:count" where the hash encodes which probe fonts
  // are installed, detected via measureText width/height deltas against generic
  // baseline families. Never returns the raw font list.
  function detectFonts() {
    var probes = ["Arial", "Helvetica", "Times New Roman", "Courier New", "Georgia",
      "Verdana", "Tahoma", "Trebuchet MS", "Impact", "Comic Sans MS", "Segoe UI",
      "Roboto", "Ubuntu", "Cantarell", "Menlo", "Monaco", "Consolas", "Calibri",
      "Cambria", "Garamond", "Palatino", "Franklin Gothic", "Century Gothic",
      "Lucida Console", "MS Gothic", "Meiryo", "SimSun", "Noto Sans", "Open Sans",
      "Liberation Sans", "DejaVu Sans", "Droid Sans", "PT Sans", "Source Sans Pro",
      "Fira Sans", "Inter", "Helvetica Neue", "Andale Mono", "Courier", "Roboto Mono"];
    var baseFonts = ["monospace", "sans-serif", "serif"];
    var span = document.createElement("span");
    span.style.position = "absolute";
    span.style.left = "-9999px";
    span.style.fontSize = "72px";
    span.textContent = "mmmmmmmmmmlli";
    document.body.appendChild(span);
    var base = {};
    for (var i = 0; i < baseFonts.length; i++) {
      span.style.fontFamily = baseFonts[i];
      base[baseFonts[i]] = { w: span.offsetWidth, h: span.offsetHeight };
    }
    var bits = "", count = 0;
    for (var j = 0; j < probes.length; j++) {
      var found = false;
      for (var k = 0; k < baseFonts.length; k++) {
        span.style.fontFamily = "'" + probes[j] + "'," + baseFonts[k];
        if (span.offsetWidth !== base[baseFonts[k]].w || span.offsetHeight !== base[baseFonts[k]].h) {
          found = true;
          break;
        }
      }
      bits += found ? "1" : "0";
      if (found) count++;
    }
    document.body.removeChild(span);
    return hash32(bits) + ":" + count;
  }

  // audioHash renders a short OfflineAudioContext buffer and hashes the summed
  // output magnitude — a stable per-device/-browser value.
  function audioHash() {
    var AC = window.OfflineAudioContext || window.webkitOfflineAudioContext;
    if (!AC) return Promise.reject();
    var ctx = new AC(1, 5000, 44100);
    var osc = ctx.createOscillator();
    osc.type = "triangle";
    osc.frequency.value = 10000;
    var comp = ctx.createDynamicsCompressor();
    osc.connect(comp);
    comp.connect(ctx.destination);
    osc.start(0);
    return ctx.startRendering().then(function (buf) {
      var data = buf.getChannelData(0);
      var acc = 0;
      for (var i = 0; i < data.length; i++) acc += Math.abs(data[i]);
      return hash32(acc.toString());
    });
  }

  if (tasks.length) {
    // Send after async collectors resolve, but never block past 800ms.
    var timeout = new Promise(function (res) { setTimeout(res, 800); });
    Promise.race([Promise.all(tasks), timeout]).then(send);
  } else {
    send();
  }
})();
