// Compact QR encoder (byte mode) for OAuth URLs. Offline, no deps.
// Implements a practical subset: versions 1-15, EC level M, mask 0-7.
(function (global) {
  "use strict";

  var EXP = new Array(512);
  var LOG = new Array(256);
  (function () {
    var x = 1;
    for (var i = 0; i < 255; i++) {
      EXP[i] = x;
      LOG[x] = i;
      x <<= 1;
      if (x & 0x100) x ^= 0x11d;
    }
    for (var j = 255; j < 512; j++) EXP[j] = EXP[j - 255];
  })();

  function gmul(a, b) {
    if (!a || !b) return 0;
    return EXP[LOG[a] + LOG[b]];
  }

  function rsGen(deg) {
    var poly = [1];
    for (var i = 0; i < deg; i++) {
      var next = new Array(poly.length + 1).fill(0);
      for (var j = 0; j < poly.length; j++) {
        next[j] ^= poly[j];
        next[j + 1] ^= gmul(poly[j], EXP[i]);
      }
      poly = next;
    }
    return poly;
  }

  function rsEncode(data, ecLen) {
    var gen = rsGen(ecLen);
    var res = data.slice().concat(new Array(ecLen).fill(0));
    for (var i = 0; i < data.length; i++) {
      var coef = res[i];
      if (!coef) continue;
      for (var j = 0; j < gen.length; j++) {
        res[i + j] ^= gmul(gen[j], coef);
      }
    }
    return res.slice(data.length);
  }

  // version: [ecPerBlock, g1Blocks, g1Data, g2Blocks, g2Data]
  var SPECS = {
    1: [10, 1, 16, 0, 0],
    2: [16, 1, 28, 0, 0],
    3: [26, 1, 44, 0, 0],
    4: [18, 2, 32, 0, 0],
    5: [24, 2, 43, 0, 0],
    6: [16, 4, 27, 0, 0],
    7: [18, 4, 31, 0, 0],
    8: [22, 2, 38, 2, 39],
    9: [22, 3, 36, 2, 37],
    10: [26, 4, 43, 1, 44],
    11: [30, 1, 50, 4, 51],
    12: [22, 6, 36, 2, 37],
    13: [22, 8, 37, 1, 38],
    14: [24, 4, 40, 5, 41],
    15: [24, 5, 41, 5, 42],
  };
  var ALIGN = {
    1: [], 2: [6, 18], 3: [6, 22], 4: [6, 26], 5: [6, 30], 6: [6, 34],
    7: [6, 22, 38], 8: [6, 24, 42], 9: [6, 26, 46], 10: [6, 28, 50],
    11: [6, 30, 54], 12: [6, 32, 58], 13: [6, 34, 62],
    14: [6, 26, 46, 66], 15: [6, 26, 48, 70],
  };

  function sizeOf(v) { return 17 + 4 * v; }

  function encode(text) {
    var data = Array.from(new TextEncoder().encode(text));
    var version = 0, spec = null;
    for (var v = 1; v <= 15; v++) {
      var s = SPECS[v];
      var cap = s[1] * s[2] + s[3] * s[4];
      var countBits = v >= 10 ? 16 : 8;
      var need = Math.ceil((4 + countBits + 8 * data.length) / 8);
      if (need <= cap) { version = v; spec = s; break; }
    }
    if (!version) throw new Error("QR payload too long");

    var capBytes = spec[1] * spec[2] + spec[3] * spec[4];
    var bits = [];
    function put(val, n) {
      for (var i = n - 1; i >= 0; i--) bits.push((val >>> i) & 1);
    }
    put(4, 4);
    put(data.length, version >= 10 ? 16 : 8);
    for (var i = 0; i < data.length; i++) put(data[i], 8);
    for (var t = 0; t < 4 && bits.length < capBytes * 8; t++) bits.push(0);
    while (bits.length % 8) bits.push(0);
    var cw = [];
    for (var b = 0; b < bits.length; b += 8) {
      var x = 0;
      for (var k = 0; k < 8; k++) x = (x << 1) | bits[b + k];
      cw.push(x);
    }
    var pad = [0xec, 0x11], pi = 0;
    while (cw.length < capBytes) cw.push(pad[pi++ % 2]);

    var blocks = [], off = 0, ecLen = spec[0];
    for (var g1 = 0; g1 < spec[1]; g1++) {
      var d1 = cw.slice(off, off + spec[2]); off += spec[2];
      blocks.push({ d: d1, e: rsEncode(d1, ecLen) });
    }
    for (var g2 = 0; g2 < spec[3]; g2++) {
      var d2 = cw.slice(off, off + spec[4]); off += spec[4];
      blocks.push({ d: d2, e: rsEncode(d2, ecLen) });
    }
    var maxD = Math.max(spec[2], spec[4]);
    var final = [];
    for (var di = 0; di < maxD; di++) {
      for (var bi = 0; bi < blocks.length; bi++) {
        if (di < blocks[bi].d.length) final.push(blocks[bi].d[di]);
      }
    }
    for (var ei = 0; ei < ecLen; ei++) {
      for (var bj = 0; bj < blocks.length; bj++) final.push(blocks[bj].e[ei]);
    }

    var n = sizeOf(version);
    var m = [], reserved = [];
    for (var r = 0; r < n; r++) {
      m.push(new Array(n).fill(0));
      reserved.push(new Array(n).fill(0));
    }
    function finder(r0, c0) {
      for (var dr = -1; dr <= 7; dr++) {
        for (var dc = -1; dc <= 7; dc++) {
          var rr = r0 + dr, cc = c0 + dc;
          if (rr < 0 || cc < 0 || rr >= n || cc >= n) continue;
          var on =
            (dr >= 0 && dr <= 6 && (dc === 0 || dc === 6)) ||
            (dc >= 0 && dc <= 6 && (dr === 0 || dr === 6)) ||
            (dr >= 2 && dr <= 4 && dc >= 2 && dc <= 4);
          m[rr][cc] = on ? 1 : 0;
          reserved[rr][cc] = 1;
        }
      }
    }
    finder(0, 0); finder(0, n - 7); finder(n - 7, 0);
    for (var ti = 8; ti < n - 8; ti++) {
      var on2 = ti % 2 === 0 ? 1 : 0;
      if (!reserved[6][ti]) { m[6][ti] = on2; reserved[6][ti] = 1; }
      if (!reserved[ti][6]) { m[ti][6] = on2; reserved[ti][6] = 1; }
    }
    var centers = ALIGN[version] || [];
    for (var ai = 0; ai < centers.length; ai++) {
      for (var aj = 0; aj < centers.length; aj++) {
        var ar = centers[ai], ac = centers[aj];
        if (reserved[ar][ac]) continue;
        if ((ar < 9 && ac < 9) || (ar < 9 && ac > n - 9) || (ar > n - 9 && ac < 9)) continue;
        for (var adr = -2; adr <= 2; adr++) {
          for (var adc = -2; adc <= 2; adc++) {
            var rr2 = ar + adr, cc2 = ac + adc;
            var on3 = adr === -2 || adr === 2 || adc === -2 || adc === 2 || (adr === 0 && adc === 0);
            m[rr2][cc2] = on3 ? 1 : 0;
            reserved[rr2][cc2] = 1;
          }
        }
      }
    }
    m[n - 8][8] = 1; reserved[n - 8][8] = 1;
    for (var fi = 0; fi <= 8; fi++) {
      reserved[8][fi] = 1;
      reserved[fi][8] = 1;
    }
    for (var fj = 0; fj < 8; fj++) {
      reserved[8][n - 1 - fj] = 1;
      reserved[n - 1 - fj][8] = 1;
    }

    var dataBits = [];
    for (var ci = 0; ci < final.length; ci++) {
      for (var cb = 7; cb >= 0; cb--) dataBits.push((final[ci] >> cb) & 1);
    }
    var idx = 0, upward = true;
    for (var col = n - 1; col > 0; col -= 2) {
      if (col === 6) col = 5;
      for (var i2 = 0; i2 < n; i2++) {
        var row = upward ? n - 1 - i2 : i2;
        var cols = [col, col - 1];
        for (var cii = 0; cii < 2; cii++) {
          var c = cols[cii];
          if (c < 0 || reserved[row][c]) continue;
          var bit = idx < dataBits.length ? dataBits[idx++] : 0;
          m[row][c] = bit;
        }
      }
      upward = !upward;
    }

    function maskFn(mask, r, c) {
      switch (mask) {
        case 0: return (r + c) % 2 === 0;
        case 1: return r % 2 === 0;
        case 2: return c % 3 === 0;
        case 3: return (r + c) % 3 === 0;
        case 4: return (Math.floor(r / 2) + Math.floor(c / 3)) % 2 === 0;
        case 5: return ((r * c) % 2) + ((r * c) % 3) === 0;
        case 6: return (((r * c) % 2) + ((r * c) % 3)) % 2 === 0;
        case 7: return (((r + c) % 2) + ((r * c) % 3)) % 2 === 0;
      }
      return false;
    }

    // format info tables EC=M
    var FORMAT_M = [
      0x5412, 0x5125, 0x5e7c, 0x5b4b, 0x45f9, 0x40ce, 0x4f97, 0x4aa0,
    ];
    // Actually standard table for M:
    // mask0: 101010000010010 = 0x5412 is WRONG for M - that's often listed for L
    // Correct M format:
    var FORMAT_M_CORRECT = [
      0x5412, // commonly cited - we'll use BCH computed values below
    ];

    function formatBits(ecLevelBits, mask) {
      // ecLevelBits: L=01, M=00, Q=11, H=10
      var data = (ecLevelBits << 3) | mask;
      var rem = data;
      for (var i = 0; i < 10; i++) rem = (rem << 1) ^ ((rem >>> 9) * 0x537);
      var bits = ((data << 10) | rem) ^ 0x5412;
      return bits;
    }

    function placeFormat(mat, mask) {
      var bits = formatBits(0b00, mask); // level M
      var arr = [];
      for (var i = 14; i >= 0; i--) arr.push((bits >>> i) & 1);
      var pos1 = [
        [8, 0], [8, 1], [8, 2], [8, 3], [8, 4], [8, 5], [8, 7], [8, 8],
        [7, 8], [5, 8], [4, 8], [3, 8], [2, 8], [1, 8], [0, 8],
      ];
      for (var p = 0; p < 15; p++) mat[pos1[p][0]][pos1[p][1]] = arr[p];
      for (var q = 0; q < 7; q++) mat[n - 1 - q][8] = arr[q];
      for (var w = 0; w < 8; w++) mat[8][n - 8 + w] = arr[7 + w];
    }

    function applyMaskAndFormat(mask) {
      var mat = m.map(function (row) { return row.slice(); });
      for (var r = 0; r < n; r++) {
        for (var c = 0; c < n; c++) {
          if (!reserved[r][c] && maskFn(mask, r, c)) mat[r][c] ^= 1;
        }
      }
      placeFormat(mat, mask);
      return mat;
    }

    function penalty(mat) {
      var p = 0, i, j, run, color;
      for (i = 0; i < n; i++) {
        run = 1; color = mat[i][0];
        for (j = 1; j < n; j++) {
          if (mat[i][j] === color) run++;
          else { if (run >= 5) p += 3 + (run - 5); color = mat[i][j]; run = 1; }
        }
        if (run >= 5) p += 3 + (run - 5);
      }
      for (j = 0; j < n; j++) {
        run = 1; color = mat[0][j];
        for (i = 1; i < n; i++) {
          if (mat[i][j] === color) run++;
          else { if (run >= 5) p += 3 + (run - 5); color = mat[i][j]; run = 1; }
        }
        if (run >= 5) p += 3 + (run - 5);
      }
      for (i = 0; i < n - 1; i++) {
        for (j = 0; j < n - 1; j++) {
          var s = mat[i][j] + mat[i][j + 1] + mat[i + 1][j] + mat[i + 1][j + 1];
          if (s === 0 || s === 4) p += 3;
        }
      }
      return p;
    }

    var best = null, bestP = Infinity;
    for (var mk = 0; mk < 8; mk++) {
      var mat = applyMaskAndFormat(mk);
      var pen = penalty(mat);
      if (pen < bestP) { bestP = pen; best = mat; }
    }
    return best;
  }

  function draw(canvas, text, opts) {
    opts = opts || {};
    var mat;
    try {
      mat = encode(text);
    } catch (e) {
      console.error(e);
      return false;
    }
    var n = mat.length;
    var scale = opts.scale || 4;
    var margin = opts.margin != null ? opts.margin : 2;
    var size = (n + margin * 2) * scale;
    canvas.width = size;
    canvas.height = size;
    var ctx = canvas.getContext("2d");
    ctx.fillStyle = "#ffffff";
    ctx.fillRect(0, 0, size, size);
    ctx.fillStyle = "#000000";
    for (var r = 0; r < n; r++) {
      for (var c = 0; c < n; c++) {
        if (mat[r][c]) {
          ctx.fillRect((c + margin) * scale, (r + margin) * scale, scale, scale);
        }
      }
    }
    return true;
  }

  global.HyQR = { encode: encode, draw: draw };
})(window);
