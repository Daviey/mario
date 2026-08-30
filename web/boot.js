(async () => {
  const canvas = document.getElementById('game');
  const ctx = canvas.getContext('2d');
  let imgData = null;
  // Loading screen: honest progress from the wasm download, drawn as the
  // game itself. Every color, sprite, tile and glyph below is a port of
  // the Go renderer (render/render.go, sprites.go, font.go), so the boot
  // screen and the title screen are one continuous world: the camera
  // scrolls right as bytes arrive, Mario runs, and he reaches the
  // flagpole at 100%. Fades out on the first painted frame.
  const loader = document.getElementById('loader');
  const pctEl = document.getElementById('pct');
  const phaseEl = document.getElementById('phase');
  const boot = document.getElementById('boot');
  const bc = boot.getContext('2d');
  const RM = matchMedia('(prefers-reduced-motion: reduce)').matches;

  // Palette — render.go NewPalette, exact hexes.
  const PC = {
    sky:'#5C94FC', gLight:'#F8B060', gMid:'#C84C0C', gDark:'#7C2800',
    brickL:'#D86818', brickD:'#6B2B00', qBG:'#FC9838', qDim:'#E08018',
    qHi:'#FFD9A0', qMark:'#FFF8E0', pipeL:'#80D010', pipeM:'#00A800',
    pipeD:'#004400', pole:'#98E858', flag:'#E4221B', dark:'#1A0E04',
    mario:'#FF3B30', skin:'#FFC89E', overall:'#2B5DD7', white:'#FFFFFF',
  };
  // Sprite art — render/sprites.go, verbatim.
  const sprMario = ['.RRR.','RRRRR','SSDSS','RBBBR','.D.D.'];
  const sprCloud = ['....WWWW....','..WWWWWWWW..','.WWWWWWWWWW.','..WWWWWWWW..'];
  const sprHill  = ['...EE...','.EGGGGE.','GGGGGGGG'];
  const sprBush  = ['.EGGE.','GGGGGG'];
  const artMap = { R:PC.mario, S:PC.skin, D:PC.dark, B:PC.overall,
                   W:PC.white, E:PC.pipeL, G:PC.pipeM };
  // Favicon + iOS home-screen icon, drawn from this same art at boot —
  // no binary asset to keep in sync, CSP-safe (img-src data:). 32 px
  // halves cleanly onto 16 px tabs.
  const setIcon = (rel, size) => {
    const c = document.createElement('canvas');
    c.width = c.height = size;
    const ig = c.getContext('2d');
    ig.fillStyle = PC.sky;
    ig.fillRect(0, 0, size, size);
    const s = Math.floor(size / 6); // sprite is 5 px wide → ~83% coverage
    const ox = (size - 5*s) >> 1, oy = (size - 5*s) >> 1;
    for (let r = 0; r < sprMario.length; r++)
      for (let col = 0; col < sprMario[r].length; col++) {
        const paint = artMap[sprMario[r][col]];
        if (!paint) continue;
        ig.fillStyle = paint;
        ig.fillRect(ox + col*s, oy + r*s, s, s);
      }
    let l = document.querySelector('link[rel="' + rel + '"]');
    if (!l) { l = document.createElement('link'); l.rel = rel; document.head.appendChild(l); }
    l.href = c.toDataURL('image/png');
  };
  setIcon('icon', 32);
  setIcon('apple-touch-icon', 180);
  const px = (x, y, w, h, c) => { bc.fillStyle = c; bc.fillRect(x|0, y|0, w, h); };
  const drawArt = (art, x, y, sc) => {
    for (let r = 0; r < art.length; r++)
      for (let c = 0; c < art[r].length; c++) {
        const col = artMap[art[r][c]];
        if (col) px(x + c*sc, y + r*sc, sc, sc, col);
      }
  };

  // 3×5 font — render/font.go font3x5, verbatim (1px tracking).
  const FONT = {
    A:['###','#.#','###','#.#','#.#'], B:['##.','#.#','##.','#.#','##.'],
    C:['###','#..','#..','#..','###'], D:['##.','#.#','#.#','#.#','##.'],
    E:['###','#..','##.','#..','###'], F:['###','#..','##.','#..','#..'],
    G:['###','#..','#.#','#.#','###'], H:['#.#','#.#','###','#.#','#.#'],
    I:['###','.#.','.#.','.#.','###'], J:['..#','..#','..#','#.#','###'],
    K:['#.#','#.#','##.','#.#','#.#'], L:['#..','#..','#..','#..','###'],
    M:['#.#','###','###','#.#','#.#'], N:['##.','#.#','#.#','#.#','#.#'],
    O:['###','#.#','#.#','#.#','###'], P:['###','#.#','###','#..','#..'],
    Q:['###','#.#','###','..#','..#'], R:['###','#.#','##.','#.#','#.#'],
    S:['###','#..','###','..#','###'], T:['###','.#.','.#.','.#.','.#.'],
    U:['#.#','#.#','#.#','#.#','###'], V:['#.#','#.#','#.#','#.#','.#.'],
    W:['#.#','#.#','###','###','#.#'], X:['#.#','#.#','.#.','#.#','#.#'],
    Y:['#.#','#.#','###','.#.','.#.'], Z:['###','..#','.#.','#..','###'],
    '0':['###','#.#','#.#','#.#','###'], '1':['.#.','##.','.#.','.#.','###'],
    '2':['###','..#','.#.','#..','###'], '3':['###','..#','.##','..#','###'],
    '4':['#.#','#.#','###','..#','..#'], '5':['###','#..','###','..#','###'],
    '6':['#..','#..','###','#.#','###'], '7':['###','..#','.#.','.#.','.#.'],
    '8':['###','#.#','###','#.#','###'], '9':['###','#.#','###','..#','..#'],
    '!':['.#.','.#.','.#.','...','.#.'], '.':['...','...','...','...','.#.'],
    '-':['...','...','###','...','...'], '+':['...','.#.','###','.#.','...'],
    '/':['..#','..#','.#.','#..','#..'], ':':['...','.#.','...','.#.','...'],
    '?':['###','..#','.#.','...','.#.'], ' ':['...','...','...','...','...'],
  };
  const textW = (s, sc) => s.length ? (4*s.length - 1) * sc : 0;
  const drawText = (s, x, y, c, sc) => {
    let cx = x;
    for (const ch of s) {
      const g = FONT[ch] || FONT['?'];
      for (let r = 0; r < 5; r++)
        for (let col = 0; col < 3; col++)
          if (g[r][col] === '#') px(cx + col*sc, y + r*sc, sc, sc, c);
      cx += 4*sc;
    }
  };
  // drawCenterShadowPx: 1px drop shadow keeps text readable over clouds.
  const centerShadow = (s, y, c, sc) => {
    const x = (160 - textW(s, sc)) / 2;
    drawText(s, x + sc, y + sc, PC.dark, sc);
    drawText(s, x, y, c, sc);
  };
  // Sky-level title lines draw without a shadow: it fills the letters'
  // counters and wrecks legibility (see render drawTitlePx).
  const centerText = (s, y, c, sc) => drawText(s, (160 - textW(s, sc)) / 2, y, c, sc);

  // Tiles — render/sprites.go draw*, 4px each, verbatim ports.
  const tileGround = (x, y, tx, ty, open) => {
    px(x, y, 4, 4, PC.gMid);
    if (open) px(x, y, 4, 1, PC.gLight);
    px(x + 3, y + 1, 1, 3, PC.gDark);
    px(x + ((tx + ty) % 2) + 1, y + 2, 1, 1, PC.gDark);
    px(x + ((tx*3 + ty) % 2), y + 3, 1, 1, PC.gDark);
  };
  const tileBrick = (x, y, tx) => {
    px(x, y, 4, 4, PC.brickL);
    px(x, y + 3, 4, 1, PC.brickD);
    px(x + (tx % 2 ? 3 : 1), y, 1, 4, PC.brickD);
    px(x, y, 1, 1, PC.qHi);
  };
  const tileQ = (x, y, bright) => {
    px(x, y, 4, 4, bright ? PC.qBG : PC.qDim);
    px(x, y, 1, 1, PC.qHi); px(x + 3, y, 1, 1, PC.brickD);
    px(x, y + 3, 1, 1, PC.brickD); px(x + 3, y + 3, 1, 1, PC.brickD);
    px(x + 1, y, 2, 1, PC.qMark); px(x, y + 1, 1, 1, PC.qMark);
    px(x + 3, y + 1, 1, 1, PC.qMark); px(x + 1, y + 3, 1, 1, PC.qMark);
  };
  const pipeShaft = (x, y, h) => {
    px(x + 1, y, 2, h, PC.pipeL);
    px(x + 3, y, 3, h, PC.pipeM);
    px(x + 6, y, 1, h, PC.pipeD);
  };
  const tilePipeLip = (x, y) => { // drawPipe lip tile: rim + shaft stub
    px(x, y, 8, 1, PC.pipeL); px(x + 4, y, 4, 1, PC.pipeM);
    px(x, y, 1, 1, PC.pipeM); px(x + 7, y, 1, 1, PC.pipeD);
    px(x, y + 1, 8, 1, PC.pipeD);
    pipeShaft(x, y + 2, 2);
  };

  // Level layout, deterministic in the tile index: 124 tiles of world,
  // flag at 90, blocks and pipes ending well before it.
  const TILE = 4, WORLD_T = 124, FLAG_T = 90, MARIO_X = 56;
  const isQ      = (t) => t > 4 && t < 84 && (t % 8 === 2 || t % 8 === 3 || t % 8 === 5);
  const isBrick  = (t) => t > 4 && t < 84 && (t % 16 >= 9 && t % 16 <= 12);
  const isPipeL  = (t) => t > 6 && t < 80 && t % 29 === 7;
  const isHill   = (t) => t < 88 && t % 9 === 0;
  const isBush   = (t) => t < 88 && t % 13 === 5;

  let cam = 0, frac = 0, frame = 0, loaderDone = false;
  // End with the pole at Mario's right edge — he grabs it, like the game.
  const camTarget = () => frac * (FLAG_T*TILE - MARIO_X - 12);
  const renderScene = () => {
    px(0, 0, 160, 56, PC.sky);
    const bright = RM ? true : frame % 48 < 24; // game flash cadence
    // Clouds first (sky dressing), drifting at 0.4× parallax.
    for (let k = 0; k < 3; k++) {
      const span = 184;
      const x = ((k*73 - cam*0.4) % span + span) % span - 12;
      // Clouds only on open sky: keep the flagpole clear (cloudBlocked).
      const fx0 = FLAG_T*4 - cam;
      if (fx0 > x - 16 && fx0 < x + 20) continue;
      drawArt(sprCloud, x, 22 + (k % 2)*5, 1);
    }
    const cx = Math.floor(cam);
    const t0 = Math.floor(cx / 4) - 2, t1 = Math.floor((cx + 160) / 4) + 2;
    for (let t = t0; t <= t1; t++) {
      if (t < 0 || t >= WORLD_T) continue;
      const x = t*4 - cx;
      tileGround(x, 48, t, 10, true);
      tileGround(x, 52, t, 11, false);
      if (isHill(t)) drawArt(sprHill, x, 45, 1);
      if (isBush(t)) drawArt(sprBush, x, 46, 1);
      if (isQ(t)) tileQ(x, 32, bright);
      else if (isBrick(t)) tileBrick(x, 32, t);
      if (isPipeL(t)) { tilePipeLip(x, 40); pipeShaft(x, 44, 4); }
    }
    // Flagpole: pole rows 24..48, finial + pennant above (drawFlagTop),
    // clear of the title band (logo + subtitle end at row 21).
    const fx = FLAG_T*4 - cx;
    if (fx > -8 && fx < 168) {
      for (let ty = 6; ty < 12; ty++) px(fx + 1, ty*4, 1, 4, PC.pole);
      px(fx + 1, 24, 1, 1, PC.pipeL);
      px(fx + 1, 25, 1, 3, PC.pole);
      px(fx, 25, 1, 1, PC.flag); px(fx - 1, 26, 1, 1, PC.flag); px(fx, 26, 1, 1, PC.flag);
      px(fx - 2, 27, 1, 1, PC.flag); px(fx - 1, 27, 1, 1, PC.flag); px(fx, 27, 1, 1, PC.flag);
    }
    // Mario, feet on the ground line; 1px run bob until the pole.
    const running = frac < 1 && !loaderDone;
    const bob = running && !RM && ((frame >> 3) & 1) ? 1 : 0;
    drawArt(sprMario, MARIO_X, 38 - bob, 2);
    // Title cascade, exactly like the in-game title screen.
    centerText('MARIO', 2, PC.flag, 2);
    centerText('SUPER CLI EDITION', 17, PC.white, 1);
  };
  const tick = () => {
    frame++;
    cam += (camTarget() - cam) * 0.12;
    if (Math.abs(camTarget() - cam) < 0.1) cam = camTarget();
    renderScene();
    if (!loaderDone && !RM) requestAnimationFrame(tick);
  };
  const sizeBoot = () => { // integer scale, like the game canvas
    const k = Math.max(2, Math.floor(Math.min(640, innerWidth * 0.92) / 160));
    boot.style.width = 160*k + 'px';
    boot.style.height = 56*k + 'px';
  };
  sizeBoot();
  window.addEventListener('resize', sizeBoot);
  const MB = 1048576;
  const fmt = (n) => (n / MB).toFixed(1) + ' MB';
  const setProgress = (p, label) => {
    p = Math.max(0, Math.min(100, p));
    frac = p / 100;
    pctEl.textContent = Math.floor(p) + '%';
    if (label) phaseEl.textContent = label;
    if (RM) { cam = camTarget(); renderScene(); }
  };
  const loaderReady = () => {
    if (loaderDone) return;
    loaderDone = true;
    frac = 1; cam = camTarget();
    setProgress(100, 'ready');
    renderScene();
    loader.classList.add('done');
    setTimeout(() => loader.remove(), 600);
  };
  tick();

  // The game pushes raw RGB pixels; paint them straight into the canvas.
  // object-fit: contain + pixelated keeps square pixels, uniform scale,
  // and fills the window (letterbox only if the aspect differs).
  window.marioFrame = (w, h, rgb) => {
    if (canvas.width !== w || canvas.height !== h) {
      canvas.width = w; canvas.height = h;
      imgData = ctx.createImageData(w, h);
    }
    const d = imgData.data;
    for (let i = 0, j = 0; i < d.length; i += 4, j += 3) {
      d[i] = rgb[j]; d[i+1] = rgb[j+1]; d[i+2] = rgb[j+2]; d[i+3] = 255;
    }
    ctx.putImageData(imgData, 0, 0);
    loaderReady(); // first painted frame = game is up
  };

  // The game pushes leaderboard UI state as JSON; render it as real DOM
  // text (crisp browser fonts, not canvas pixels). mode "off" hides it.
  const boardEl = document.getElementById('board');
  const escHtml = (v) => String(v).replace(/[&<>"']/g, (c) => ({
    '&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;' }[c]));
  const pad = (n, w) => String(n).padStart(w, '0');
  const IS_TOUCH = () => document.body.classList.contains('touch');
  window.marioBoard = (json) => {
    let st;
    try { st = JSON.parse(json); } catch { return; }
    if (!st || st.mode === 'off') {
      boardEl.classList.remove('on-touch'); boardEl.classList.add('off');
      if (window.setPadMode) window.setPadMode('game');
      return;
    }
    boardEl.classList.remove('off');
    if (IS_TOUCH()) boardEl.classList.add('on-touch');
    if (window.setPadMode) window.setPadMode(st.mode);
    let html = '';
    if (st.mode === 'ask') {
      html = '<h3>GAME OVER</h3>'
        + '<p class="score">SCORE ' + escHtml(pad(st.score, 6)) + '</p>'
        + (st.best > 0 ? '<p class="hint">BEST ' + escHtml(pad(st.best, 6)) + '</p>' : '')
        + '<p class="gold blink">SUBMIT TO LEADERBOARD?</p>'
        + (IS_TOUCH() ? '' : '<p>Y YES&nbsp;&nbsp;&nbsp;N NO</p>');
    } else if (st.mode === 'about') {
      html = '<h3>SUPER CLI MARIO</h3>'
        + '<p class="gold">a fan-made terminal platformer</p>'
        + '<p>unofficial fan art · not affiliated with nintendo</p>'
        + '<p class="hint">mario is a trademark of nintendo</p>'
        + '<p>plays in your terminal · over ssh · in the browser</p>'
        + (IS_TOUCH() ? '' : '<p class="hint blink">I CLOSE</p>');
    } else if (st.mode === 'entry') {
      const name = escHtml(st.name || '');
      const padN = '&nbsp;'.repeat(Math.max(0, 9 - name.length));
      html = '<h3>GAME OVER</h3>'
        + '<p class="score">SCORE ' + escHtml(pad(st.score, 6)) + '</p>'
        + (st.best > 0 ? '<p class="hint">BEST ' + escHtml(pad(st.best, 6)) + '</p>' : '')
        + '<p class="gold">ENTER NAME</p>'
        + '<p class="field">[' + name + '<span class="blink">_</span>' + padN + ']</p>'
        + (IS_TOUCH() ? '<p class="hint">TAP LETTERS BELOW</p>' : '<p class="hint">ENTER OK&nbsp;&nbsp;&nbsp;BS DEL&nbsp;&nbsp;&nbsp;ESC BACK</p>');
    } else { // board
      let body;
      if (st.loading) body = '<p class="blink">LOADING...</p>';
      else if (!st.rows || !st.rows.length) body = '<p>NO SCORES YET</p>';
      else body = '<table><tr class="hdr"><th>#</th><th>NAME</th><th>SCORE</th><th>LV</th></tr>' + st.rows.map((r) =>
        '<tr' + (r.mine ? ' class="mine"' : '') + '><td class="rk">' + escHtml(r.rank)
        + '</td><td class="nm">' + escHtml(r.name) + (r.verified ? ' <span class="vfy">✓</span>' : '')
        + '</td><td class="sc">' + escHtml(r.score)
        + '</td><td class="lv">L' + escHtml(r.level || 1) + '</td></tr>').join('') + '</table>';
      const foot = (st.status ? '<p class="gold">' + escHtml(st.status) + '</p>' : '')
        + (st.title ? (IS_TOUCH() ? '' : '<p class="hint blink">L CLOSE</p>')
                    : (IS_TOUCH() ? '' : '<p class="hint blink">R RESTART&nbsp;&nbsp;&nbsp;Q QUIT</p>'));
      html = '<h3>' + (st.daily ? 'DAILY LEADERBOARD' : 'LEADERBOARD') + '</h3>' + body
        + (st.rank > 0 ? '<p class="gold">YOU ARE #' + escHtml(st.rank) + '</p>' : '')
        + foot;
    }
    boardEl.innerHTML = '<div class="card">' + html + '</div>';
  };

  // ---- Sound: window.marioSfx is the game's whole audio layer — a tiny
  // WebAudio chiptune synth (square/triangle oscillators + one noise
  // buffer; no assets, no deps). The AudioContext is created lazily and
  // resumed on the first user gesture (autoplay policy). ----
  let ac = null, master = null, noiseBuf = null;
  let muted = false;
  try { muted = localStorage.getItem('mario-muted') === '1'; } catch {}
  const ensureAudio = () => {
    if (ac) { if (ac.state === 'suspended') ac.resume().catch(() => {}); return; }
    const Ctor = window.AudioContext || window.webkitAudioContext;
    if (!Ctor) return;
    try {
      ac = new Ctor();
      master = ac.createGain();
      master.gain.value = 0.25;
      master.connect(ac.destination); // the one edge the whole synth hangs on
      noiseBuf = ac.createBuffer(1, (ac.sampleRate * 0.5) | 0, ac.sampleRate);
      const d = noiseBuf.getChannelData(0);
      for (let i = 0; i < d.length; i++) d[i] = Math.random() * 2 - 1;
    } catch { ac = null; }
  };
  const unlock = () => ensureAudio();
  window.addEventListener('pointerdown', unlock, { once: true, passive: true });
  window.addEventListener('keydown', unlock, { once: true });
  // One oscillator voice: type + optional pitch glide + decay envelope.
  const note = (type, f0, f1, dur, t, vol) => {
    const o = ac.createOscillator(), g = ac.createGain();
    o.type = type;
    o.frequency.setValueAtTime(f0, t);
    if (f1 !== f0) o.frequency.exponentialRampToValueAtTime(f1, t + dur);
    g.gain.setValueAtTime(vol, t);
    g.gain.exponentialRampToValueAtTime(0.001, t + dur);
    o.connect(g).connect(master);
    o.start(t); o.stop(t + dur + 0.02);
  };
  const burst = (dur, t, freq, vol) => { // filtered noise hit
    const s = ac.createBufferSource(); s.buffer = noiseBuf;
    const f = ac.createBiquadFilter(); f.type = 'bandpass';
    f.frequency.value = freq; f.Q.value = 0.8;
    const g = ac.createGain();
    g.gain.setValueAtTime(vol, t);
    g.gain.exponentialRampToValueAtTime(0.001, t + dur);
    s.connect(f).connect(g).connect(master);
    s.start(t); s.stop(t + dur + 0.02);
  };
  const arp = (freqs, step, vol) => {
    const t = ac.currentTime;
    for (let i = 0; i < freqs.length; i++)
      note('square', freqs[i], freqs[i], step * 0.95, t + i * step, vol);
  };
  const SFX = {
    jump: () => note('square', 150, 750, 0.15, ac.currentTime, 0.5),
    coin: () => { const t = ac.currentTime;
      note('square', 988, 988, 0.08, t, 0.5); note('square', 1319, 1319, 0.35, t + 0.08, 0.45); },
    stomp: () => { const t = ac.currentTime;
      note('square', 250, 80, 0.12, t, 0.5); burst(0.1, t, 400, 0.35); },
    kick: () => note('square', 400, 150, 0.1, ac.currentTime, 0.5),
    brick: () => burst(0.18, ac.currentTime, 800, 0.5),
    bump: () => note('triangle', 90, 90, 0.09, ac.currentTime, 0.7),
    powerup: () => arp([523, 659, 784, 1046, 1318, 1568], 0.055, 0.4),
    oneup: () => arp([659, 784, 659, 523, 587, 784], 0.09, 0.4),
    die: () => note('square', 600, 100, 0.6, ac.currentTime, 0.5),
    flag: () => note('square', 1000, 200, 0.5, ac.currentTime, 0.45),
    clear: () => arp([523, 523, 523, 659, 784, 1046], 0.09, 0.45),
    tick: () => note('square', 1319, 1319, 0.03, ac.currentTime, 0.15),
    hurry: () => { const t = ac.currentTime; // 880/660 alternating ×3
      for (let i = 0; i < 3; i++) {
        note('square', 880, 880, 0.05, t + i * 0.12, 0.4);
        note('square', 660, 660, 0.05, t + i * 0.12 + 0.06, 0.4);
      } },
    fire: () => note('square', 900, 300, 0.09, ac.currentTime, 0.35),
    pause: () => { const t = ac.currentTime;
      note('square', 660, 660, 0.05, t, 0.4); note('square', 880, 880, 0.05, t + 0.06, 0.4); },
    gameover: () => arp([523, 392, 330, 262], 0.2, 0.45),
    win: () => arp([523, 659, 784, 1046, 784, 1046], 0.12, 0.45),
  };
  const VIBE = { stomp: 20, coin: 10, powerup: 40, oneup: [30, 40, 30], die: 80 };
  const lastSfx = {};
  window.marioSfx = (name) => {
    const play = SFX[name];
    if (!play || muted) return;
    const now = performance.now(); // rate-limit repeats (tick can spam)
    if (now - (lastSfx[name] || 0) < 40) return;
    lastSfx[name] = now;
    const v = VIBE[name];
    if (v !== undefined && document.body.classList.contains('touch') && navigator.vibrate)
      navigator.vibrate(v);
    ensureAudio();
    if (ac) { try { play(); } catch {} }
  };

  // Top-bar controls: mute (persisted) and fullscreen + landscape lock.
  // tabindex=-1 + blur() keeps game keys off the buttons.
  const muteBtn = document.getElementById('mute-btn');
  muteBtn.textContent = muted ? '🔇' : '🔊';
  muteBtn.addEventListener('click', () => {
    muted = !muted;
    try { localStorage.setItem('mario-muted', muted ? '1' : '0'); } catch {}
    muteBtn.textContent = muted ? '🔇' : '🔊';
    muteBtn.blur();
  });
  const lockLandscape = () => {
    try {
      if (screen.orientation && screen.orientation.lock)
        screen.orientation.lock('landscape').catch(() => {});
    } catch {}
  };
  const fsBtn = document.getElementById('fs-btn');
  fsBtn.addEventListener('click', () => {
    if (document.fullscreenElement) {
      if (document.exitFullscreen) document.exitFullscreen().catch(() => {});
    } else if (document.documentElement.requestFullscreen) {
      document.documentElement.requestFullscreen().then(lockLandscape).catch(() => {});
    }
    fsBtn.blur();
  });
  document.addEventListener('fullscreenchange', () => {
    if (document.fullscreenElement) lockLandscape();
  });

  // Title-screen hook: the DAILY pad button only exists at the title,
  // and the top-bar hint line carries the fan-game disclaimer there
  // instead of the controls (mirrors the terminal status line swap in
  // render.drawStatus).
  const BAR_CONTROLS = document.querySelector('#bar .dim');
  if (BAR_CONTROLS) BAR_CONTROLS.dataset.controls = BAR_CONTROLS.textContent;
  const BAR_ABOUT = 'unofficial fan game · not affiliated with nintendo';
  window.marioTitle = (at) => {
    const b = document.getElementById('daily-btn');
    if (b) b.hidden = !at;
    if (BAR_CONTROLS) BAR_CONTROLS.textContent = at ? BAR_ABOUT : BAR_CONTROLS.dataset.controls;
  };

  // Rotate hint: portrait touch only, dismissible for the session.
  const rotateEl = document.getElementById('rotate');
  try { if (sessionStorage.getItem('mario-rotated') === '1') rotateEl.classList.add('off'); } catch {}
  const rotBtn = document.getElementById('rotate-x');
  rotBtn.addEventListener('click', () => {
    rotateEl.classList.add('off');
    try { sessionStorage.setItem('mario-rotated', '1'); } catch {}
    rotBtn.blur();
  });

  // PWA: register the service worker once the page is fully loaded —
  // http(s) only; file:// has neither service workers nor the fetch.
  window.addEventListener('load', () => {
    if (location.protocol !== 'http:' && location.protocol !== 'https:') return;
    if ('serviceWorker' in navigator) navigator.serviceWorker.register('./sw.js').catch(() => {});
  });

  // Integer uniform scaling, like every polished pixel game: pick the
  // largest integer k where the game's max height fits, then size the
  // viewport so the scaled frame fills the width. Every world pixel is
  // exactly k device pixels — perfectly uniform, no banding possible.
  const PIX = 4, HUD = 8, STATUS = 7, MAXW_T = 60, MAXH_T = 15, MINH_T = 4;
  const sizeViewport = () => {
    const dpr = window.devicePixelRatio || 1;
    const availW = Math.floor(document.getElementById('stage').clientWidth * dpr);
    const availH = Math.floor(document.getElementById('stage').clientHeight * dpr);
    // Tiles of vertical view the window can show at scale 1.
    let tilesH = Math.max(MINH_T, Math.min(MAXH_T, Math.floor((availH - HUD - STATUS) / PIX)));
    let k = Math.max(1, Math.floor(availH / (HUD + tilesH * PIX + STATUS)));
    // If width forces fewer tiles at this scale, shrink view before scale.
    while (k > 1 && Math.floor(availW / (k * PIX)) < 16) k--;
    const tilesW = Math.max(16, Math.min(MAXW_T, Math.floor(availW / (k * PIX))));
    const pxW = tilesW * PIX, pxH = HUD + tilesH * PIX + STATUS;
    if (window.marioSize) window.marioSize(pxW, pxH);
    canvas.style.width = (pxW * k / dpr) + 'px';
    canvas.style.height = (pxH * k / dpr) + 'px';
  };

  // Keyboard -> terminal-style sequences the game's input mapper already
  // parses. Every key — arrows AND letters — goes out as an explicit
  // kitty-protocol press/release pair. A press is held until its release
  // arrives; a bare letter byte would decay after the mapper's ~0.2s
  // legacy silence window, so holding a key (or a pad button) has to be
  // an explicit held press, not a text byte.
  const PRESS = {
    ArrowLeft: '\x1b[1;1:1D', ArrowRight: '\x1b[1;1:1C',
    ArrowUp: '\x1b[1;1:1A', ArrowDown: '\x1b[1;1:1B',
  };
  const RELEASE = {
    ArrowLeft: '\x1b[1;1:3D', ArrowRight: '\x1b[1;1:3C',
    ArrowUp: '\x1b[1;1:3A', ArrowDown: '\x1b[1;1:3B',
  };
  const kittyPress = (key) => '\x1b[' + key.charCodeAt(0) + ';1:1u';
  const release = (code, key) =>
    RELEASE[code] || '\x1b[' + key.charCodeAt(0) + ';1:3u';
  window.addEventListener('keydown', (e) => {
    if (e.ctrlKey && e.key === 'c') { window.marioFeed('\x03'); e.preventDefault(); return; }
    // A physical keypress on a machine the pointer media queries misjudge
    // as touch-only (e.g. Firefox/Wayland with a touchscreen) is the
    // strongest signal a real keyboard exists — drop the on-screen pad
    // for good (the 🎛 bar button brings it back).
    if (document.body.classList.contains('touch') && padPref() === 'auto') {
      setPadPref('off'); disableTouch();
    }
    if (e.repeat) return; // a press stays held until its release; repeats unneeded
    const seq = PRESS[e.code]
      || (e.key.length === 1 ? kittyPress(e.key) : null)
      || (e.key === 'Enter' ? '\r' : null)
      || (e.key === 'Escape' ? '\x1b' : null);
    if (seq) { window.marioFeed(seq); e.preventDefault(); }
  });
  window.addEventListener('keyup', (e) => {
    if (e.key.length === 1) window.marioFeed(release(e.code, e.key));
    else if (RELEASE[e.code]) window.marioFeed(RELEASE[e.code]);
  });

  // Touch controls
  const padEl = document.getElementById('pad');
  padEl.innerHTML = `<div id="pad-game" style="display:contents">
    <div class="grp stick-grp"><div id="stick" class="stick" aria-label="move: tilt to run, pull down to duck"><div class="stick-knob"></div></div></div>
    <div class="grp mid-grp"><button class="pbtn mid-btn" data-tap="p" aria-label="pause" style="letter-spacing:.2em">II</button><button class="pbtn mid-btn" data-tap="k" aria-label="give up (die)">☠</button><button class="pbtn mid-btn" data-tap="r" aria-label="restart">&#10227;</button><button class="pbtn mid-btn" data-tap="&#13;">START</button><button class="pbtn mid-btn" data-tap="l">SCORES</button><button class="pbtn mid-btn" id="arrange-btn" aria-label="move controls: drag them, then tap again to lock">&#10022;</button><button class="pbtn mid-btn" id="daily-btn" data-tap="d" hidden>DAILY</button></div>
    <div class="grp act-grp"><button class="pbtn act-btn b-btn" data-key="x" aria-label="run and fire">B</button><button class="pbtn act-btn a-btn" data-key="w" aria-label="jump">A</button></div>
  </div>
  <div id="pad-ask" style="display:none; gap:16px"><button class="pbtn btn-yes" data-tap="y">YES</button><button class="pbtn btn-no" data-tap="n">NO</button></div>
  <div id="pad-board" style="display:none; gap:16px"><button class="pbtn btn-no" data-tap="l">CLOSE</button><button class="pbtn btn-yes" data-tap="r">RESTART</button></div>
  <div id="pad-about" style="display:none; gap:16px"><button class="pbtn btn-no" data-tap="i">CLOSE</button></div>
  <div id="pad-entry" style="display:none"></div>`;
  const padViews = { game: document.getElementById('pad-game'), ask: document.getElementById('pad-ask'), board: document.getElementById('pad-board'), about: document.getElementById('pad-about'), entry: document.getElementById('pad-entry') };
  window.setPadMode = (m) => {
    padEl.className = m === 'entry' ? 'keypad' : m;
    for (const k in padViews) padViews[k].style.display = k === m ? (m === 'game' ? 'contents' : m === 'entry' ? '' : 'flex') : 'none';
  };
  
  const kbd = 'QWERTYUIOPASDFGHJKLZXCVBNM1234567890.-'.split('');
  kbd.forEach(c => { const b = document.createElement('button'); b.className = 'pbtn'; b.textContent = c; b.dataset.tap = c; padViews.entry.appendChild(b); });
  const bs = document.createElement('button'); bs.className = 'pbtn'; bs.textContent = '⌫'; bs.dataset.tap = '\x7f'; padViews.entry.appendChild(bs);
  const ok = document.createElement('button'); ok.className = 'pbtn ok'; ok.textContent = 'OK'; ok.dataset.tap = '\r'; padViews.entry.appendChild(ok);
  const esc = document.createElement('button'); esc.className = 'pbtn'; esc.textContent = 'ESC'; esc.dataset.tap = '\x1b'; padViews.entry.appendChild(esc);

  const arranging = () => padEl.classList.contains('arrange');

  const bindPad = (b) => {
    b.addEventListener('contextmenu', e => e.preventDefault());
    const k = b.dataset.key;
    if (k) {
      const up = () => { if (!b.classList.contains('held')) return; b.classList.remove('held'); if (window.marioFeed) window.marioFeed(release(k, k)); };
      b.addEventListener('pointerdown', e => { e.preventDefault(); if (arranging()) return; b.setPointerCapture?.(e.pointerId); b.classList.add('held'); if (navigator.vibrate) navigator.vibrate(4); if (window.marioFeed) window.marioFeed(PRESS[k] || kittyPress(k)); });
      b.addEventListener('pointerup', up); b.addEventListener('pointercancel', up); b.addEventListener('lostpointercapture', up);
    }
    const t = b.dataset.tap;
    if (t) b.addEventListener('pointerdown', e => { e.preventDefault(); if (arranging()) return; if (navigator.vibrate) navigator.vibrate(4); if (window.marioFeed) window.marioFeed(t); b.classList.add('held'); setTimeout(()=>b.classList.remove('held'), 80); });
  };
  padEl.querySelectorAll('.pbtn').forEach(bindPad);

  // Virtual joystick: tilt left/right to run, pull down to duck.
  // Directions latch as held kitty presses with edge transitions only —
  // exactly the press/release semantics the d-pad buttons had.
  const stick = document.getElementById('stick');
  const knob = stick.querySelector('.stick-knob');
  const DIRKEY = { left: 'a', right: 'd', down: 's' };
  let heldDirs = new Set();
  const setDirs = (next) => {
    for (const d of heldDirs) if (!next.has(d) && window.marioFeed) window.marioFeed(release(DIRKEY[d], DIRKEY[d]));
    for (const d of next) if (!heldDirs.has(d)) { if (navigator.vibrate) navigator.vibrate(4); if (window.marioFeed) window.marioFeed(kittyPress(DIRKEY[d])); }
    heldDirs = next;
  };
  const dirsFor = (dx, dy) => {
    const s = new Set();
    if (dx < -0.30) s.add('left'); else if (dx > 0.30) s.add('right');
    if (dy > 0.45) s.add('down');
    return s;
  };
  stick.addEventListener('contextmenu', e => e.preventDefault());
  stick.addEventListener('pointerdown', (e) => {
    e.preventDefault();
    if (arranging()) return;
    stick.setPointerCapture?.(e.pointerId);
    stick.classList.add('active');
    const rect = stick.getBoundingClientRect();
    const r = stick.clientWidth / 2 - 10;
    const move = (ev) => {
      let dx = (ev.clientX - (rect.left + rect.width / 2)) / r;
      let dy = (ev.clientY - (rect.top + rect.height / 2)) / r;
      const m = Math.hypot(dx, dy);
      if (m > 1) { dx /= m; dy /= m; }
      knob.style.transform = 'translate(' + (dx * r) + 'px,' + (dy * r) + 'px)';
      setDirs(dirsFor(dx, dy));
    };
    move(e);
    const up = () => {
      stick.removeEventListener('pointermove', move);
      setDirs(new Set());
      knob.style.transform = '';
      stick.classList.remove('active');
    };
    stick.addEventListener('pointermove', move);
    stick.addEventListener('pointerup', up, { once: true });
    stick.addEventListener('pointercancel', up, { once: true });
    stick.addEventListener('lostpointercapture', up, { once: true });
  });

  // ---- control layout: drag controls in arrange mode (the &#10022; pill);
  // positions persist as viewport fractions so rotation and device
  // changes keep them under the same thumbs. ----
  const draggables = [
    ['stick', document.getElementById('stick')],
    ['act', document.querySelector('#pad .act-grp')],
    ['mid', document.querySelector('#pad .mid-grp')],
  ];
  const layoutKey = 'mario.pad.layout';
  const loadLayout = () => { try { return JSON.parse(localStorage.getItem(layoutKey)) || {}; } catch { return {}; } };
  const applyLayout = () => {
    const saved = loadLayout();
    for (const [name, el] of draggables) {
      const pos = saved[name];
      if (!pos) continue;
      const r = el.getBoundingClientRect();
      const x = Math.max(0, Math.min(pos[0] * (innerWidth - r.width), innerWidth - r.width));
      const y = Math.max(0, Math.min(pos[1] * (innerHeight - r.height), innerHeight - r.height));
      el.style.left = x + 'px';
      el.style.top = y + 'px';
      el.style.right = 'auto';
      el.style.bottom = 'auto';
      el.style.transform = 'none';
      el.style.marginTop = '0';
    }
  };
  const saveLayout = () => {
    const saved = loadLayout();
    for (const [name, el] of draggables) {
      const r = el.getBoundingClientRect();
      // offsetLeft/Top are transform-free; a default-positioned mid row
      // still carries translateX(-50%), so derive from the rect and undo
      // the known translate instead of reading offsetLeft mid-transform.
      const tx = el.style.transform === '' || el.style.transform === 'none' ? 0 : r.width / 2;
      const x = r.x + tx, y = r.y;
      saved[name] = [
        r.width < innerWidth ? x / (innerWidth - r.width) : 0,
        r.height < innerHeight ? y / (innerHeight - r.height) : 0,
      ];
    }
    try { localStorage.setItem(layoutKey, JSON.stringify(saved)); } catch {}
  };
  const arrangeBtn = document.getElementById('arrange-btn');
  const bindArrangeDrag = (el) => {
    el.addEventListener('pointerdown', (e) => {
      if (!arranging()) return;
      e.preventDefault();
      e.stopPropagation();
      el.setPointerCapture?.(e.pointerId);
      el.classList.add('held');
      const start = { x: e.clientX, y: e.clientY, l: el.offsetLeft, t: el.offsetTop };
      const move = (ev) => {
        const w = el.offsetWidth, h = el.offsetHeight;
        el.style.left = Math.max(0, Math.min(start.l + ev.clientX - start.x, innerWidth - w)) + 'px';
        el.style.top = Math.max(0, Math.min(start.t + ev.clientY - start.y, innerHeight - h)) + 'px';
        el.style.right = 'auto';
        el.style.bottom = 'auto';
        el.style.transform = 'none';
        el.style.marginTop = '0';
      };
      const up = () => {
        el.removeEventListener('pointermove', move);
        el.classList.remove('held');
        if (navigator.vibrate) navigator.vibrate(10);
      };
      el.addEventListener('pointermove', move);
      el.addEventListener('pointerup', up, { once: true });
      el.addEventListener('pointercancel', up, { once: true });
    });
  };
  for (const [, el] of draggables) bindArrangeDrag(el);
  arrangeBtn.addEventListener('pointerdown', (e) => {
    e.preventDefault();
    e.stopPropagation();
    const on = !padEl.classList.contains('arrange');
    padEl.classList.toggle('arrange', on);
    arrangeBtn.classList.toggle('arranging', on);
    arrangeBtn.classList.add('held');
    setTimeout(() => arrangeBtn.classList.remove('held'), 120);
    if (!on) saveLayout();
    if (navigator.vibrate) navigator.vibrate(on ? 12 : 20);
  });
  applyLayout();
  addEventListener('resize', applyLayout);

  // Pad visibility: auto by default (pointer media queries), or the
  // user's explicit persisted choice — the queries alone misjudge some
  // setups (Firefox on Wayland with a touchscreen reports no fine
  // pointer), and a physical keypress flips auto to off (see the keydown
  // handler above). ?touch remains a hard debug override.
  const padPref = () => { try { return localStorage.getItem('mario.pad') || 'auto'; } catch { return 'auto'; } };
  const setPadPref = v => { try { localStorage.setItem('mario.pad', v); } catch {} };
  const enableTouch = () => {
    if (document.body.classList.contains('touch')) return;
    document.body.classList.add('touch');
    document.getElementById('note').innerHTML = 'tap START to play · <a href="#">restart</a>';
    sizeViewport();
  };
  const disableTouch = () => {
    if (!document.body.classList.contains('touch')) return;
    document.body.classList.remove('touch');
    document.getElementById('note').innerHTML = 'click the game to focus · <a href="#">restart</a>';
    sizeViewport();
  };
  const applyPad = () => {
    if (new URLSearchParams(location.search).has('touch')) { enableTouch(); return; }
    const p = padPref();
    if (p === 'on') enableTouch();
    else if (p === 'off') disableTouch();
    // auto: pointer-class gated. Some devices misjudge (Android WebView
    // and several Samsung phones report a fine pointer alongside their
    // touchscreen) — a coarse pointer with real touch points is a phone,
    // so let that combination win over the fine-pointer veto.
    else if (!matchMedia('(any-pointer: fine)').matches
        || (matchMedia('(any-pointer: coarse)').matches && navigator.maxTouchPoints > 0)) enableTouch();
    else disableTouch();
  };
  applyPad();
  // 🎛 bar button: toggle relative to what is currently shown; an
  // explicit choice beats both detection and the auto-keypress rule.
  const padBtn = document.getElementById('pad-btn');
  padBtn.addEventListener('click', () => {
    if (document.body.classList.contains('touch')) { setPadPref('off'); disableTouch(); }
    else { setPadPref('on'); enableTouch(); }
    padBtn.blur();
  });

  // Start the game module; main() runs its own 60 Hz loop. mario.wasm is
  // a few MB, so fetch it manually for real byte progress, then
  // instantiate from the buffer.
  try {
    const res = await fetch('./mario.wasm');
    if (!res.ok) throw new Error('HTTP ' + res.status);
    let bytes;
    if (res.body) {
      const total = +res.headers.get('Content-Length') || 0;
      const reader = res.body.getReader();
      const chunks = [];
      let got = 0;
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        chunks.push(value);
        got += value.length;
        // Bar tracks the true download fraction; later phases are
        // labels only, so the numbers never contradict each other.
        // If the server hides the length, creep toward done instead.
        const frac = total ? Math.min(1, got / total) : got / (got + 1.5 * MB);
        setProgress(frac * 100,
          'downloading ' + fmt(got) + (total ? ' / ' + fmt(total) : ''));
      }
      bytes = new Uint8Array(got);
      let off = 0;
      for (const c of chunks) { bytes.set(c, off); off += c.length; }
    } else {
      bytes = await res.arrayBuffer();
    }
    setProgress(100, 'compiling');
    const go = new Go();
    const { instance } = await WebAssembly.instantiate(bytes, go.importObject);
    setProgress(100, 'starting');
    go.run(instance);
  } catch (e) {
    loaderDone = true; // freeze the scene; the error text carries the news
    phaseEl.classList.add('err');
    phaseEl.style.color = '#ff7b72';
    phaseEl.textContent = 'failed to load mario.wasm: ' + e +
      ' — serve over http(s), not file://';
    return;
  }
  sizeViewport();
  window.addEventListener('resize', () => setTimeout(sizeViewport, 100));
  if (window.visualViewport) visualViewport.addEventListener('resize', () => setTimeout(sizeViewport, 100));
  document.addEventListener('gesturestart', e => e.preventDefault());
  document.body.addEventListener('click', () => window.focus());
})();

// Restart link: the note element's HTML is rewritten in touch mode, so
// bind once on the stable parent and delegate (no inline handlers — the
// CSP no longer allows them).
document.getElementById('note').addEventListener('click', (e) => {
  if (e.target && e.target.tagName === 'A') {
    e.preventDefault();
    location.reload();
  }
});
