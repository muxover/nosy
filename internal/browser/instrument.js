(function () {
  'use strict';
  var send = window.__nosy;
  try { delete window.__nosy; } catch (e) {}
  if (typeof send !== 'function') return;

  var frame = '';
  try { frame = location.origin; } catch (e) {}

  var PER_SCRIPT = 8;
  var seen = {};

  // the first stack frame with a web address is the page script that made
  // the call; frames of this file have none
  function callerScript() {
    var m = /(https?:\/\/[^\s()]+?):\d+:\d+/.exec(new Error().stack || '');
    return m ? m[1] : 'inline';
  }

  function emit(tech, detail) {
    var script = callerScript();
    var key = tech + '|' + script;
    seen[key] = (seen[key] || 0) + 1;
    if (seen[key] > PER_SCRIPT) return;
    try { send(JSON.stringify({ t: 'fp', tech: tech, script: script, frame: frame, detail: detail || '' })); } catch (e) {}
  }

  function wrap(proto, name, before) {
    var orig = proto && proto[name];
    if (typeof orig !== 'function') return;
    Object.defineProperty(proto, name, {
      value: function () {
        try { before.call(this, arguments); } catch (e) {}
        return Reflect.apply(orig, this, arguments);
      },
      writable: true, configurable: true
    });
  }

  // canvas: a read-back counts when the canvas is at least 16x16 and
  // something was drawn on it that varies by machine (text of 10 or more
  // distinct characters, or 2 or more colours), unless the script also
  // used save/restore or listened for events (a drawing app, not a probe)
  var canvases = new WeakMap();
  function canvasState(c) {
    var s = canvases.get(c);
    if (!s) { s = { chars: new Set(), colors: new Set(), skip: false }; canvases.set(c, s); }
    return s;
  }
  var ctx2d = window.CanvasRenderingContext2D && CanvasRenderingContext2D.prototype;
  if (ctx2d) {
    var fillStyle = Object.getOwnPropertyDescriptor(ctx2d, 'fillStyle');
    var strokeStyle = Object.getOwnPropertyDescriptor(ctx2d, 'strokeStyle');
    var colorOf = function (ctx, d) { try { return String(d.get.call(ctx)); } catch (e) { return ''; } };
    var text = function (style) {
      return function (args) {
        var s = canvasState(this.canvas);
        var t = String(args[0]);
        for (var i = 0; i < t.length; i++) s.chars.add(t[i]);
        s.colors.add(colorOf(this, style));
      };
    };
    var shape = function (style) {
      return function () { canvasState(this.canvas).colors.add(colorOf(this, style)); };
    };
    wrap(ctx2d, 'fillText', text(fillStyle));
    wrap(ctx2d, 'strokeText', text(strokeStyle));
    wrap(ctx2d, 'fillRect', shape(fillStyle));
    wrap(ctx2d, 'fill', shape(fillStyle));
    wrap(ctx2d, 'strokeRect', shape(strokeStyle));
    wrap(ctx2d, 'stroke', shape(strokeStyle));
    wrap(ctx2d, 'save', function () { canvasState(this.canvas).skip = true; });
    wrap(ctx2d, 'restore', function () { canvasState(this.canvas).skip = true; });

    var checkRead = function (canvas, w, h, api) {
      var s = canvases.get(canvas);
      if (!s || s.skip || w < 16 || h < 16) return;
      if (s.chars.size >= 10 || s.colors.size >= 2) emit('canvas', api + ' ' + w + 'x' + h);
    };
    wrap(ctx2d, 'getImageData', function (a) { checkRead(this.canvas, a[2], a[3], 'getImageData'); });
    if (window.HTMLCanvasElement) {
      var cp = HTMLCanvasElement.prototype;
      wrap(cp, 'toDataURL', function () { checkRead(this, this.width, this.height, 'toDataURL'); });
      wrap(cp, 'toBlob', function () { checkRead(this, this.width, this.height, 'toBlob'); });
      wrap(cp, 'addEventListener', function () { canvasState(this).skip = true; });
    }

    // fonts: 50 or more different font values set and 50 or more text
    // measurements on one canvas context
    var fonts = new WeakMap();
    var fontDesc = Object.getOwnPropertyDescriptor(ctx2d, 'font');
    if (fontDesc && fontDesc.set) {
      Object.defineProperty(ctx2d, 'font', {
        get: fontDesc.get,
        set: function (v) {
          try {
            var f = fonts.get(this);
            if (!f) { f = { set: new Set(), calls: 0, done: false }; fonts.set(this, f); }
            f.set.add(String(v));
          } catch (e) {}
          return fontDesc.set.call(this, v);
        },
        configurable: true, enumerable: true
      });
      wrap(ctx2d, 'measureText', function () {
        var f = fonts.get(this);
        if (!f || f.done) return;
        f.calls++;
        if (f.calls >= 50 && f.set.size >= 50) { f.done = true; emit('fonts', f.set.size + ' fonts measured'); }
      });
    }
  }

  // fonts measured through the page: element sizes read back while the
  // script swaps font-family on the element, 50 different families
  var dom = { fams: new Set(), reads: 0, done: false };
  ['offsetWidth', 'offsetHeight'].forEach(function (p) {
    var d = window.HTMLElement && Object.getOwnPropertyDescriptor(HTMLElement.prototype, p);
    if (!d || !d.get) return;
    Object.defineProperty(HTMLElement.prototype, p, {
      get: function () {
        if (!dom.done) {
          try {
            var fam = this.style && this.style.fontFamily;
            if (fam) {
              dom.fams.add(fam);
              dom.reads++;
              if (dom.reads >= 50 && dom.fams.size >= 50) { dom.done = true; emit('fonts', dom.fams.size + ' font families measured'); }
            }
          } catch (e) {}
        }
        return d.get.call(this);
      },
      configurable: true, enumerable: true
    });
  });

  // webgl: the unmasked renderer string was read
  ['WebGLRenderingContext', 'WebGL2RenderingContext'].forEach(function (n) {
    if (window[n]) wrap(window[n].prototype, 'getParameter', function (a) {
      if (a[0] === 37446) emit('webgl', 'UNMASKED_RENDERER_WEBGL');
    });
  });

  // audio: a buffer rendered by an OfflineAudioContext was read back
  var rendered = new WeakSet();
  if (window.OfflineAudioContext) {
    var oac = OfflineAudioContext.prototype;
    var start = oac.startRendering;
    Object.defineProperty(oac, 'startRendering', {
      value: function () {
        var p = Reflect.apply(start, this, arguments);
        try { if (p && p.then) p.then(function (b) { rendered.add(b); }, function () {}); } catch (e) {}
        return p;
      },
      writable: true, configurable: true
    });
    var rb = window.OfflineAudioCompletionEvent && Object.getOwnPropertyDescriptor(OfflineAudioCompletionEvent.prototype, 'renderedBuffer');
    if (rb && rb.get) {
      Object.defineProperty(OfflineAudioCompletionEvent.prototype, 'renderedBuffer', {
        get: function () { var b = rb.get.call(this); try { rendered.add(b); } catch (e) {} return b; },
        configurable: true, enumerable: true
      });
    }
  }
  if (window.AudioBuffer) {
    ['getChannelData', 'copyFromChannel'].forEach(function (n) {
      wrap(AudioBuffer.prototype, n, function () { if (rendered.has(this)) emit('audio', 'OfflineAudioContext'); });
    });
  }

  // cookies written from script, for saying which script set a cookie
  var cookie = window.Document && Object.getOwnPropertyDescriptor(Document.prototype, 'cookie');
  if (cookie && cookie.set) {
    Object.defineProperty(Document.prototype, 'cookie', {
      get: cookie.get,
      set: function (v) {
        try {
          send(JSON.stringify({ t: 'cookie', name: String(v).split('=')[0].trim(), script: callerScript(), frame: frame }));
        } catch (e) {}
        return cookie.set.call(this, v);
      },
      configurable: true, enumerable: true
    });
  }

  // new windows the page asks for: Nosy closes them, and notes the request
  var open = window.open;
  if (typeof open === 'function') {
    Object.defineProperty(window, 'open', {
      value: function (url) {
        try { send(JSON.stringify({ t: 'popup', url: String(url == null ? '' : url).slice(0, 300), script: callerScript(), frame: frame })); } catch (e) {}
        return Reflect.apply(open, this, arguments);
      },
      writable: true, configurable: true
    });
  }
})();
//# sourceURL=nosy-instrument.js
