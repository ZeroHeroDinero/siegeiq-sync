// ui_html.go - the app window's page, as one self-contained string.
//
// No build step, no node_modules, no bundler, and no external requests. Not one
// stylesheet, font or script is fetched from the internet - the window works on
// a machine with no connection, and "this app opens no ports and calls nothing
// out" stays true of the interface as well as the recorder.
//
// TWO THINGS IN HERE ARE LOAD-BEARING AND EASY TO BREAK:
//
//  1. It lives inside a Go raw string literal, so there must be no backtick
//     anywhere in the markup or the script. No JavaScript template literals.
//
//  2. The page must not call into Go until the bridge exists. WebView2 injects
//     the bound functions shortly after the document is created, which can lose
//     a race against a script that runs the moment it is parsed. That race is
//     what left the first build showing "Loading..." forever. whenReady() below
//     is the fix, and removing it brings the bug straight back.
package main

const uiHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>SiegeIQ Sync</title>
<style>
:root{
  --bg:#080F18; --bg2:#0B1522;
  --panel:#0E1B29; --panel2:#132538; --panel3:#18304612;
  --line:#1B2F44; --line2:#26445F;
  --text:#EAF3F9; --dim:#8FA8BC; --faint:#5E7488;
  --accent:#2FC3E8; --accent2:#1B93B4; --accentGlow:#2FC3E82E;
  --good:#4FD199; --warn:#F0B45C; --bad:#F0705C;
  --r:12px;
}
*{box-sizing:border-box;margin:0;padding:0}
html,body{height:100%}
body{
  background:
    radial-gradient(1100px 520px at 12% -12%, #12314733 0%, transparent 62%),
    radial-gradient(900px 460px at 108% 4%, #2FC3E814 0%, transparent 58%),
    var(--bg);
  color:var(--text);
  font:14px/1.6 "Segoe UI Variable Text","Segoe UI",system-ui,sans-serif;
  padding:26px 30px 60px; -webkit-font-smoothing:antialiased;
}
::-webkit-scrollbar{width:11px;height:11px}
::-webkit-scrollbar-track{background:transparent}
::-webkit-scrollbar-thumb{background:#1E3549;border-radius:8px;border:3px solid transparent;background-clip:padding-box}
::-webkit-scrollbar-thumb:hover{background:#2B4A66;background-clip:padding-box;border:3px solid transparent}

@keyframes rise{from{opacity:0;transform:translateY(9px)}to{opacity:1;transform:none}}
@keyframes pulse{0%,100%{box-shadow:0 0 0 0 var(--accentGlow)}50%{box-shadow:0 0 0 7px transparent}}
@keyframes shimmer{0%{background-position:-380px 0}100%{background-position:380px 0}}
@keyframes spin{to{transform:rotate(360deg)}}
.rise{animation:rise .42s cubic-bezier(.22,.7,.3,1) both}
.d1{animation-delay:.04s}.d2{animation-delay:.09s}.d3{animation-delay:.14s}.d4{animation-delay:.19s}

header{display:flex;align-items:center;gap:14px;margin-bottom:22px}
.logo{width:34px;height:34px;border-radius:9px;background:linear-gradient(140deg,var(--accent),var(--accent2));
      display:flex;align-items:center;justify-content:center;flex:none}
.logo svg{width:19px;height:19px;fill:#04121B}
h1{font-size:19px;font-weight:600;letter-spacing:-.2px;line-height:1.15}
.sub{color:var(--faint);font-size:12px;letter-spacing:.2px}
.state{margin-left:auto;display:flex;align-items:center;gap:9px;background:#0E1B29CC;border:1px solid var(--line);
       border-radius:22px;padding:8px 16px 8px 13px;font-size:13px;backdrop-filter:blur(8px);
       transition:border-color .3s,background .3s;max-width:46%}
.state.live{border-color:#2FC3E866;background:#0F2734CC}
.dot{width:9px;height:9px;border-radius:50%;background:var(--faint);flex:none;transition:background .3s}
.dot.on{background:var(--good);animation:pulse 2.1s infinite}
.dot.off{background:var(--bad)}
.dot.idle{background:var(--warn)}
#stateline{white-space:nowrap;overflow:hidden;text-overflow:ellipsis}

h2{font-size:11.5px;font-weight:600;color:var(--faint);text-transform:uppercase;letter-spacing:1.1px;
   margin:30px 0 12px;display:flex;align-items:center;gap:11px}
h2:after{content:"";flex:1;height:1px;background:linear-gradient(90deg,var(--line),transparent)}

.cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(196px,1fr));gap:12px}
.card{background:linear-gradient(168deg,#10202FEE,#0C1826EE);border:1px solid var(--line);border-radius:var(--r);
      padding:15px 17px;transition:transform .22s cubic-bezier(.22,.7,.3,1),border-color .22s}
.card:hover{transform:translateY(-2px);border-color:var(--line2)}
.card .k{color:var(--faint);font-size:10.5px;text-transform:uppercase;letter-spacing:1px;font-weight:600}
.card .v{font-size:23px;margin-top:7px;font-weight:600;letter-spacing:-.5px;line-height:1.1}
.card .v.sm{font-size:17px}
.card .s{color:var(--dim);font-size:12.5px;margin-top:4px;min-height:19px}
.bar{height:6px;background:#0A1723;border-radius:4px;margin-top:11px;overflow:hidden}
.bar i{display:block;height:100%;border-radius:4px;background:linear-gradient(90deg,var(--accent2),var(--accent));
       transition:width .7s cubic-bezier(.22,.7,.3,1)}
.bar i.hot{background:linear-gradient(90deg,#C2803A,var(--warn))}

.note{display:flex;gap:11px;align-items:flex-start;background:#0E1B29;border:1px solid var(--line);
      border-left:3px solid var(--good);border-radius:0 10px 10px 0;padding:12px 15px;margin-top:14px;
      font-size:13.5px;color:var(--dim);line-height:1.55}
.note.warn{border-left-color:var(--warn)} .note.bad{border-left-color:var(--bad)}
.note b{color:var(--text);font-weight:600}
.note svg{width:16px;height:16px;flex:none;margin-top:2px;opacity:.85}

.row{display:flex;gap:10px;flex-wrap:wrap;margin-top:16px}
button{background:#152738;color:var(--text);border:1px solid var(--line);border-radius:9px;padding:9px 16px;
       font:inherit;font-size:13.5px;font-weight:500;cursor:pointer;display:inline-flex;align-items:center;gap:8px;
       transition:background .18s,border-color .18s,transform .12s}
button:hover{background:#1B3145;border-color:var(--line2)}
button:active{transform:translateY(1px)}
button svg{width:15px;height:15px;fill:currentColor;opacity:.9}
button.primary{background:linear-gradient(140deg,var(--accent),var(--accent2));border-color:transparent;color:#04121B;font-weight:600}
button.primary:hover{filter:brightness(1.09)}
button.ghost{background:transparent}
.savegrp{display:inline-flex;align-items:center;gap:6px;flex:none;background:#0E1B29;border:1px solid var(--line);
         border-radius:11px;padding:5px 6px 5px 14px}
.savelbl{display:inline-flex;align-items:center;gap:8px;color:var(--dim);font-size:13.5px;font-weight:500;
         white-space:nowrap;padding-right:4px}
.savelbl svg{width:15px;height:15px;fill:currentColor;opacity:.9}
/* NOT .len - that class is already the duration badge overlaid on a clip
   thumbnail, and it is position:absolute. Reusing the name parked all five
   buttons on top of each other in the corner of the screen. */
button.qlen{padding:7px 12px;font-size:13px;border-radius:8px;flex:none}
/* Greyed out rather than hidden. A control that vanishes when it cannot be used
   teaches nobody that it exists; one that is visible and explains itself does. */
.savegrp.dim{opacity:.45}
.savegrp.dim button{pointer-events:none}
.savehint{color:var(--dim);font-size:12.5px;line-height:1.5;align-self:center}
/* Live send progress on a clip card. It has to be visible without being loud:
   the send now runs in the background, so this is the only thing telling the
   player anything is happening at all. */
.sending{margin-top:6px;font-size:12px;color:var(--accent);display:flex;align-items:center;gap:6px}
.sending:before{content:"";width:7px;height:7px;border-radius:50%;background:var(--accent);
  animation:pulse 1.4s infinite;flex:none}
.sending.ok{color:var(--good)} .sending.ok:before{background:var(--good);animation:none}
.sending.bad{color:var(--bad)} .sending.bad:before{background:var(--bad);animation:none}
button.danger:hover{border-color:#F0705C66;color:var(--bad);background:#2015157A}


/* ---- the shell: sidebar plus one visible section --------------------------
   body no longer scrolls as one column. The sidebar is fixed, the main pane
   scrolls on its own, and the pinned block above the sections never moves. */
.shell{display:flex;gap:0;height:100vh;margin:-26px -30px -60px;align-items:stretch}
.side{width:196px;flex:none;background:#0A1420;border-right:1px solid var(--line);
      padding:18px 11px 14px;display:flex;flex-direction:column}
.brand{display:flex;align-items:center;gap:10px;padding:0 6px 16px}
.brand h1{font-size:14px;line-height:1.2}
.brand .sub{font-size:11px}
.nav a{display:flex;align-items:center;gap:10px;padding:9px 11px;border-radius:9px;color:var(--dim);
       font-size:13.5px;margin-bottom:2px;cursor:pointer;user-select:none;
       transition:background .16s,color .16s}
.nav a:hover{background:#10202F;color:var(--text)}
.nav a.on{background:linear-gradient(140deg,#12303F,#0E1F2C);color:var(--text);font-weight:600;
          box-shadow:inset 2px 0 0 var(--accent)}
.nav .ic{width:15px;height:15px;fill:currentColor;opacity:.85;flex:none}
.nav .n{margin-left:auto;font-size:11px;color:var(--faint);font-variant-numeric:tabular-nums}
.sidefoot{margin-top:auto;padding-top:12px;border-top:1px solid var(--line);display:flex;
          flex-direction:column;gap:9px}
.sidefoot .state{margin-left:0;max-width:none;font-size:12px;padding:7px 12px}
button.wide{width:100%;justify-content:center}
.main{flex:1;min-width:0;overflow-y:auto;padding:20px 28px 40px}
.pin{padding-bottom:16px;margin-bottom:6px;border-bottom:1px solid var(--line)}
.main section{animation:rise .3s cubic-bezier(.22,.7,.3,1) both}
.main section h2:first-child{margin-top:14px}
.opts{display:grid;grid-template-columns:repeat(auto-fit,minmax(268px,1fr));gap:10px}
.opt{display:flex;gap:12px;align-items:flex-start;background:#0E1B29;border:1px solid var(--line);
     border-radius:11px;padding:13px 15px;cursor:pointer;transition:border-color .2s,background .2s,transform .16s}
.opt:hover{border-color:var(--line2);transform:translateY(-1px)}
.opt.sel{border-color:var(--accent);background:linear-gradient(150deg,#12303F,#0E1F2C)}
.rd{width:16px;height:16px;border:2px solid #3B566E;border-radius:50%;flex:none;margin-top:3px;position:relative;transition:border-color .2s}
.opt.sel .rd{border-color:var(--accent)}
.rd:after{content:"";position:absolute;inset:2.5px;border-radius:50%;background:var(--accent);
          transform:scale(0);transition:transform .2s cubic-bezier(.3,1.5,.5,1)}
.opt.sel .rd:after{transform:scale(1)}
.opt b{font-weight:600;font-size:13.5px;display:block}
.opt span{display:block;color:var(--dim);font-size:12.5px;margin-top:3px;line-height:1.5}
.opt .flag{display:inline-block;font-size:10.5px;font-weight:600;letter-spacing:.4px;text-transform:uppercase;
           color:var(--warn);border:1px solid #F0B45C55;border-radius:20px;padding:1px 7px;margin-top:7px}

.grid2{display:grid;grid-template-columns:repeat(auto-fit,minmax(224px,1fr));gap:12px}
.fld{background:#0E1B29;border:1px solid var(--line);border-radius:11px;padding:13px 15px;transition:border-color .2s}
.fld:focus-within{border-color:var(--accent)}
.hint{color:var(--dim);font-size:12.5px;margin:-4px 0 12px;line-height:1.55}
.hint code{background:#0A1723;border:1px solid var(--line);border-radius:5px;padding:1px 6px;font-size:12px}
.fld input[type=text]{font-size:12.5px}
.fld label{display:block;color:var(--faint);font-size:10.5px;text-transform:uppercase;letter-spacing:1px;font-weight:600}
.fld input,.fld select{width:100%;margin-top:8px;background:#0A1522;color:var(--text);border:1px solid var(--line);
       border-radius:8px;padding:9px 11px;font:inherit;font-size:14px;outline:none;transition:border-color .2s}
.fld input:focus,.fld select:focus{border-color:var(--accent)}
.fld .h{color:var(--faint);font-size:11.5px;margin-top:7px;line-height:1.5}

.clips{display:grid;grid-template-columns:repeat(auto-fill,minmax(268px,1fr));gap:14px}
.clip{background:#0E1B29;border:1px solid var(--line);border-radius:var(--r);overflow:hidden;
      display:flex;flex-direction:column;
      transition:transform .22s cubic-bezier(.22,.7,.3,1),border-color .22s}
.clip:hover{transform:translateY(-3px);border-color:var(--line2)}
.shot{position:relative;aspect-ratio:16/9;background:#0A1522;cursor:pointer;overflow:hidden}
.shot img{width:100%;height:100%;object-fit:cover;display:block;transition:transform .5s cubic-bezier(.22,.7,.3,1)}
.clip:hover .shot img{transform:scale(1.055)}
.shot .none{position:absolute;inset:0;display:flex;align-items:center;justify-content:center;color:var(--faint);font-size:12.5px}
.shot .ov{position:absolute;inset:0;background:linear-gradient(180deg,transparent 42%,#040B12E0);
          display:flex;align-items:flex-end;padding:11px;opacity:0;transition:opacity .25s}
.clip:hover .shot .ov{opacity:1}
.playbtn{width:38px;height:38px;border-radius:50%;background:#2FC3E8;display:flex;align-items:center;justify-content:center}
.playbtn svg{width:15px;height:15px;fill:#04121B;margin-left:2px}
.len{position:absolute;right:9px;bottom:9px;background:#040B12CC;border-radius:6px;padding:2px 8px;
     font-size:11.5px;font-weight:600;letter-spacing:.3px}
.cb{padding:12px 14px 4px;flex:1}
.ct{font-weight:600;font-size:13.5px;letter-spacing:-.1px}
.cm{color:var(--faint);font-size:12px;margin-top:4px}
.tag{display:inline-block;font-size:10.5px;font-weight:600;letter-spacing:.4px;text-transform:uppercase;
     padding:2px 8px;border-radius:20px;margin-top:9px;border:1px solid #F0B45C55;color:var(--warn)}
/* margin-top:auto pins the buttons to the bottom of every card, so a clip
   without a warning tag does not sit its controls higher than its neighbours. */
.acts{display:flex;gap:7px;padding:11px 14px 14px;margin-top:auto}
.acts button{flex:1;justify-content:center;padding:7px 4px;font-size:12.5px;white-space:nowrap}

.empty{background:#0C1826;border:1px dashed var(--line2);border-radius:var(--r);padding:40px 24px;text-align:center}
.empty svg{width:34px;height:34px;fill:#33526C;margin-bottom:12px}
.empty p{color:var(--dim);font-size:13.5px}
.empty p.small{color:var(--faint);font-size:12.5px;margin-top:6px}

.skel{background:#0E1B29;border:1px solid var(--line);border-radius:var(--r);height:104px;
  background-image:linear-gradient(100deg,#0E1B29 8%,#152838 20%,#0E1B29 34%);background-size:760px 100%;
  animation:shimmer 1.35s linear infinite}

#fatal{display:none;background:#1A1012;border:1px solid #F0705C55;border-left:3px solid var(--bad);
       border-radius:0 10px 10px 0;padding:16px 18px;margin:16px 0}
#fatal h3{font-size:15px;font-weight:600;margin-bottom:6px}
#fatal p{color:var(--dim);font-size:13.5px;line-height:1.6}

#toast{position:fixed;left:50%;bottom:26px;transform:translate(-50%,26px);background:#152838;
       border:1px solid var(--line2);border-radius:10px;padding:12px 22px;font-size:13.5px;
       opacity:0;pointer-events:none;transition:opacity .28s,transform .28s cubic-bezier(.22,.7,.3,1);
       box-shadow:0 12px 34px #0006;display:flex;align-items:center;gap:10px}
#toast.show{opacity:1;transform:translate(-50%,0)}
#toast .sp{width:14px;height:14px;border:2px solid #2FC3E840;border-top-color:var(--accent);
           border-radius:50%;animation:spin .7s linear infinite;display:none}
#toast.busy .sp{display:block}
.foot{margin-top:34px;padding-top:16px;border-top:1px solid var(--line);color:var(--faint);font-size:12.5px}
</style>
</head>
<body>

<!-- ONE PAGE PER SECTION, NOT ONE LONG PAGE.
     Everything used to live on a single column that ran to about three screens,
     which meant the save-a-clip row - the one control the recorder exists for -
     could be scrolled off the bottom by a couple of explanatory banners. The
     sidebar shows one section at a time, and the status cards plus the save row
     are PINNED above all of them, so they are on screen no matter where you are. -->
<div class="shell">

<div class="side">
  <div class="brand">
    <div class="logo"><svg viewBox="0 0 24 24"><path d="M12 2 3 6v6c0 5 3.8 9.2 9 10 5.2-.8 9-5 9-10V6l-9-4zm0 4.6 3.1 6.3H8.9L12 6.6zM8.4 15h7.2l-3.6 3.3L8.4 15z"/></svg></div>
    <div><h1>SiegeIQ Sync</h1><div class="sub" id="ver">&nbsp;</div></div>
  </div>
  <nav class="nav" id="nav">
    <a data-sec="dashboard" class="on" onclick="showSec(&#39;dashboard&#39;)"><svg class="ic" viewBox="0 0 24 24"><path d="M3 13h8V3H3v10zm0 8h8v-6H3v6zm10 0h8V11h-8v10zm0-18v6h8V3h-8z"/></svg>Dashboard</a>
    <a data-sec="clips" onclick="showSec(&#39;clips&#39;)"><svg class="ic" viewBox="0 0 24 24"><path d="M4 4h16v16H4V4zm2 2v12h12V6H6zm3 2.5v7l6-3.5-6-3.5z"/></svg>Clips<span class="n" id="clipn"></span></a>
    <a data-sec="recording" onclick="showSec(&#39;recording&#39;)"><svg class="ic" viewBox="0 0 24 24"><path d="M12 2a10 10 0 100 20 10 10 0 000-20zm0 15a5 5 0 110-10 5 5 0 010 10z"/></svg>Recording</a>
    <a data-sec="sounds" onclick="showSec(&#39;sounds&#39;)"><svg class="ic" viewBox="0 0 24 24"><path d="M3 10v4h4l5 5V5L7 10H3zm13.5 2A4.5 4.5 0 0014 7.97v8.05A4.47 4.47 0 0016.5 12z"/></svg>Sounds</a>
    <a data-sec="about" onclick="showSec(&#39;about&#39;)"><svg class="ic" viewBox="0 0 24 24"><path d="M12 2a10 10 0 100 20 10 10 0 000-20zm1 15h-2v-6h2v6zm0-8h-2V7h2v2z"/></svg>About</a>
  </nav>
  <div class="sidefoot">
    <div class="state" id="statepill"><span class="dot" id="statedot"></span><span id="stateline">Connecting...</span></div>
    <button class="ghost wide" onclick="go(&#39;goHide&#39;)">Hide to tray</button>
  </div>
</div>

<div class="main">

<div id="fatal">
  <h3>The window could not reach SiegeIQ Sync</h3>
  <p id="fatalwhy"></p>
  <div class="row"><button class="primary" onclick="location.reload()">Try again</button></div>
</div>

<!-- PINNED. Above every section, never scrolled away. -->
<div class="pin">
  <div class="cards" id="cards">
    <div class="skel"></div><div class="skel"></div><div class="skel"></div><div class="skel"></div>
  </div>
  <div class="row rise d1" style="margin-top:11px">
    <span class="savegrp">
      <span class="savelbl">
        <svg viewBox="0 0 24 24"><path d="M17 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V7l-4-4zm-5 16a3 3 0 1 1 0-6 3 3 0 0 1 0 6zm3-10H6V5h9v4z"/></svg>
        Save a clip of the last</span>
      <button class="qlen" onclick="saveLast(0.5)">30 sec</button>
      <button class="qlen" onclick="saveLast(1)">1 min</button>
      <button class="qlen primary" onclick="saveLast(2)">2 min</button>
      <button class="qlen" onclick="saveLast(5)">5 min</button>
      <button class="qlen" onclick="saveLast(10)">10 min</button>
      <button class="qlen" onclick="saveLast(45)">Whole match</button>
    </span>
    <span class="savehint" id="savehint"></span>
  </div>
</div>

<section id="sec-dashboard">
  <div id="engine"></div>
  <div id="testout"></div>
  <div class="row">
    <button onclick="go(&#39;goOpenFolder&#39;)"><svg viewBox="0 0 24 24"><path d="M10 4H4a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-8l-2-2z"/></svg>Open clips folder</button>
    <button id="pausebtn" onclick="togglePause()">Pause recording</button>
    <button id="syncbtn" onclick="toggleSync()">Pause syncing</button>
    <button onclick="captureTest()" id="testbtn">Run capture test</button>
    <button class="ghost" onclick="go(&#39;goOpenLog&#39;)">View log</button>
  </div>
</section>

<section id="sec-clips" hidden>
  <h2>Your clips</h2>
  <div id="clips"><div class="skel" style="height:210px"></div></div>
</section>

<section id="sec-recording" hidden>
  <h2>When to record</h2>
  <div class="opts" id="modes"></div>

  <h2>What to keep</h2>
  <div class="opts" id="rules"></div>

  <h2>Quality and limits</h2>
  <div class="grid2" id="fields"></div>
  <div class="row"><button class="primary" onclick="saveSettings()">Save settings</button>
    <button class="ghost" onclick="refresh()">Discard changes</button></div>
</section>

<section id="sec-sounds" hidden>
  <h2>Notification sounds</h2>
  <p class="hint">The built-in sounds are used unless you name a file here. Anything in
  <code>C:\Windows\Media</code> works, and so does any other <code>.wav</code> on your PC.
  Saving plays the sound so you can hear it straight away.</p>
  <div class="grid2" id="soundfields"></div>
  <div class="row">
    <button class="primary" onclick="saveSounds()">Save and play</button>
    <button onclick="clearSounds()">Use the built-in sounds</button>
  </div>
</section>

<section id="sec-about" hidden>
  <h2>What this app does, and does not do</h2>
  <p class="hint">SiegeIQ Sync watches one folder for the replay files Siege writes itself, and
  records your screen only while Siege is running. It never reads the game&#39;s memory, never
  touches the game process, and opens no network ports. Clips stay on this PC until you send one.</p>
  <div class="grid2" id="aboutgrid"></div>
  <div class="row">
    <button onclick="go(&#39;goOpenLog&#39;)">View log</button>
    <button onclick="go(&#39;goOpenFolder&#39;)">Open clips folder</button>
  </div>
</section>

<div class="foot">Closing this window leaves SiegeIQ Sync running in your tray. Recording and syncing carry on.</div>

</div><!-- .main -->
</div><!-- .shell -->

<div id="toast"><span class="sp"></span><span id="toasttext"></span></div>

<script>
var S={}, C=[], settings={}, connected=false;

var MODES=[
 ["siege_running","While Siege is running","Buffers the whole time you are in game. The usual choice.",""],
 ["tournament","Tournament matches only","Buffers only during a SiegeIQ tournament match.","not wired up yet"],
 ["manual","Only when I arm it","Buffers nothing until you switch it on from here.",""],
 ["off","Off","No recording at all. Syncing is unaffected.",""]
];
var RULES=[
 ["action_only","Action phase of every round","Skips the drone phase at the start of each round.",""],
 ["whole_match","The whole match","One long clip, first round through to the last.",""],
 ["last_seconds","Last seconds of each round","Just the endings, length set below.",""],
 ["my_deaths","Only rounds I died in","Falls back to the action phase until SiegeIQ can decode round detail.","needs decode"],
 ["my_kills","Only rounds I got a kill","Falls back to the action phase until SiegeIQ can decode round detail.","needs decode"],
 ["clutches","Only my clutch rounds","Falls back to the action phase until SiegeIQ can decode round detail.","needs decode"],
 ["nothing","Nothing automatically","Buffer only. You save moments by hand.",""]
];

function esc(s){var d=document.createElement("div");d.textContent=s==null?"":String(s);return d.innerHTML;}
function dur(s){s=Math.round(s||0);if(s<60)return s+"s";return Math.floor(s/60)+":"+("0"+(s%60)).slice(-2);}
// mb never trusts the number to be there. One clip missing a field used to throw
// out of the middle of the list render and leave the whole panel blank, which
// reads as "the app is broken" rather than "one clip is odd".
// gb reads a megabyte count the way a person would say it out loud. "3172 MB"
// is a number you have to convert in your head before it means anything.
function gb(n){ n=Number(n)||0; return n>=1024 ? (n/1024).toFixed(1)+" GB" : n+" MB"; }
function mb(n){ n=Number(n); return (isFinite(n)?n:0).toFixed(1); }
function report(m){ if(window.goLog){ try{ window.goLog(String(m)); }catch(e){} } }

window.onerror=function(msg,src,line){ report("window script error: "+msg+" (line "+line+")"); };

function toast(msg,busy){
  var t=document.getElementById("toast");
  document.getElementById("toasttext").textContent=msg;
  t.className="show"+(busy?" busy":"");
  clearTimeout(t._h);
  if(!busy) t._h=setTimeout(function(){t.className="";},3000);
}

// go calls into the app. EVERY call passes exactly one string and gets back
// exactly one string - see bindUI in ui_window_windows.go for why. Calling with
// no argument, or with a number, is the shape that silently never arrived, so
// the argument is coerced here rather than trusted at each call site.
function go(fn,a){
  if(!window[fn]){ report("bridge call before ready: "+fn); return Promise.resolve(""); }
  var arg=(a==null?"":String(a));
  try{
    // The catch is not optional. A binding that refuses the call - wrong number
    // of arguments being the one that actually happened - rejects this promise,
    // and an unhandled rejection settles nothing, which looks exactly like the
    // app hanging. Turning it into a logged line and an empty reply is the
    // difference between a diagnosable fault and a silent one.
    return Promise.resolve(window[fn](arg)).catch(function(e){
      report("bridge call rejected: "+fn+" - "+e);
      return "";
    });
  }
  catch(e){ report("bridge call failed: "+fn+" - "+e); return Promise.resolve(""); }
}

// hardFatal marks the one genuinely unrecoverable state: the bridge never
// appeared at all. Everything else is a slow call, and a slow call must not
// leave a red panel on screen for the rest of the evening.
var hardFatal=false;
function fatal(why){
  hardFatal=true;
  document.getElementById("fatal").style.display="block";
  document.getElementById("fatalwhy").textContent=why;
  document.getElementById("stateline").textContent="Not connected";
  document.getElementById("statedot").className="dot off";
  report("window: "+why);
}

function whenReady(cb,n){
  n=n||0;
  if(window.goStatus){ connected=true; report("window: bridge ready after "+(n*60)+"ms"); cb(); return; }
  if(n>100){ fatal("The page loaded but never reached the app. This is a bug worth reporting - the log has the detail."); return; }
  setTimeout(function(){ whenReady(cb,n+1); },60);
}

// goT is go() with a deadline. If a call into the app does not come back, the
// page must not sit silent forever waiting for it - it says which call stalled,
// writes that to the log, and carries on so the rest of the interface still
// works. Two rounds of "stuck on Connecting" happened because a single hung
// call took the whole window down with it and left no trace of which one.
function goT(fn,a,ms){
  var settled=false;
  return new Promise(function(res){
    var t=setTimeout(function(){
      if(settled) return;
      settled=true;
      report("bridge call "+fn+" is taking longer than "+((ms||20000)/1000)+"s");
      stalled(fn);
      res(null);
    },ms||20000);
    go(fn,a).then(function(v){
      // Clear the stall FIRST and unconditionally. A reply that arrives after
      // the deadline has already fired is still proof the app is alive, and it
      // is the only proof that will ever come. Dropping it on the floor because
      // a timer won a race is how the window ends up showing "the app stopped
      // answering" over a screen full of working data.
      unstalled(fn);
      if(settled) return;
      settled=true; clearTimeout(t); res(v);
    });
  });
}

// goJ is goT for the calls that carry structured data. The app sends JSON text
// and the page unpacks it here, in one place, so a malformed reply produces one
// logged line instead of an exception that stops whichever paint was running.
function goJ(fn,a,ms){
  return goT(fn,a,ms).then(function(txt){
    if(txt===null) return null;
    if(txt==="") return null;
    try{ return JSON.parse(txt); }
    catch(e){ report("bridge reply from "+fn+" was not readable: "+e); return null; }
  });
}

var stalls={};
function stalled(fn){
  stalls[fn]=true;
  var names=Object.keys(stalls).join(", ");
  document.getElementById("fatal").style.display="block";
  document.getElementById("fatalwhy").textContent=
    "This is usually a large upload holding things up, and it clears on its own. "+
    "Waiting on: "+names+". Recording and syncing are unaffected. If it stays here for "+
    "more than a minute, send me the log.";
  document.getElementById("stateline").textContent="Busy";
  document.getElementById("statedot").className="dot idle";
}

// unstalled is the half that was missing.
//
// stalled() was one-way: nothing on the page could ever take that panel back
// down. Sending a 283 MB clip occupies the bridge for long enough that one
// routine poll misses its deadline, and the window then showed "the app stopped
// answering" permanently, in red, above a completely healthy interface, while
// the upload it was complaining about finished successfully.
//
// A reply is proof. Take the panel down.
function unstalled(fn){
  if(!stalls[fn]) return;
  delete stalls[fn];
  if(Object.keys(stalls).length===0 && !hardFatal){
    document.getElementById("fatal").style.display="none";
  }
}

function refresh(){
  if(!connected) return;
  goJ("goStatus").then(function(s){ if(s){S=s;paintStatus();} });
  goJ("goSettings").then(function(x){ if(x){settings=x;paintOptions();paintFields();} });
  goJ("goSoundPrefs").then(function(x){ if(x){SND=x;paintSounds();} });
  loadClips();
}

// loadClips is separate from refresh so arriving at the Clips section can ask
// for a fresh list without re-polling everything else.
function loadClips(){
  goJ("goClips",null,15000).then(function(c){ if(c!==null){ C=c||[]; paintClips(); chaseThumbs(0); } });
}

// howCapturing describes the capture path in words a player can act on. It used
// to print the backend URL here, which was both wrong and alarming - it read as
// though gameplay were being streamed to a server. Nothing is: capture is local
// and the backend is only ever sent a clip you choose to send.
function howCapturing(){
  if(!S.recording){
    if(S.recorder_paused) return "paused from here";
    if(S.mode==="off") return "recorder switched off";
    if(S.mode==="manual") return "waiting for you to arm it";
    if(S.mode==="tournament") return "waiting for a tournament match";
    return "waiting for Siege to start";
  }
  var how=(S.capture_method==="gdigrab") ? "window capture" : "GPU screen grab";
  var chip=(S.adapter!=null&&S.adapter>=0) ? (", graphics chip "+S.adapter) : "";
  return how+chip+", saving to your PC only";
}

// chaseThumbs comes back for pictures that were not ready yet.
//
// Thumbnails are made in the background so the clip list never waits on ffmpeg.
// The half of that design that was missing is this half: nothing ever asked
// again, so a clip saved by hand showed "no preview" forever, or until the
// player happened to change a setting. It gives up after a handful of tries
// rather than polling all evening over a clip whose thumbnail genuinely cannot
// be made.
var thumbChase=0;
function chaseThumbs(n){
  var waiting=C.some(function(c){ return c.thumb_pending; });
  if(!waiting||n>5) return;
  var mine=++thumbChase;
  setTimeout(function(){
    if(mine!==thumbChase) return;
    goJ("goClips",null,15000).then(function(c){
      if(c===null) return;
      C=c||[]; paintClips(); chaseThumbs(n+1);
    });
  },1500);
}


// showSec swaps which section is on screen.
//
// hidden rather than display:none in a class, because the attribute survives
// anything the paint functions do to these elements and cannot be fought by a
// stylesheet rule somebody adds later.
var CURSEC="dashboard";
function showSec(name){
  CURSEC=name;
  var secs=document.querySelectorAll(".main section");
  for(var i=0;i<secs.length;i++){ secs[i].hidden = (secs[i].id !== "sec-"+name); }
  var links=document.querySelectorAll(".nav a");
  for(var j=0;j<links.length;j++){ links[j].className = (links[j].getAttribute("data-sec")===name)?"on":""; }
  var m=document.querySelector(".main"); if(m) m.scrollTop=0;
  // Clips are the one section worth re-fetching on arrival, because a clip
  // saved while another section was open would otherwise not be there.
  if(name==="clips") loadClips();
}

// paintAbout fills the About section from the same status object everything
// else uses, so it can never disagree with the cards.
function paintAbout(){
  var el=document.getElementById("aboutgrid");
  if(!el||!S.app_version) return;
  el.innerHTML=
     ab("Version",S.app_version)
    +ab("Capture",(S.capture_method==="gdigrab"?"window capture":"GPU screen grab")
        +(S.encoder?(", "+S.encoder):""))
    +ab("Sound",S.audio_on?"being recorded":"not being recorded")
    +ab("Clips folder",S.clip_dir||"not set")
    +ab("Buffer",S.buffer_minutes+" minutes, up to "+Math.round(S.buffer_cap_mb/1024)+" GB")
    +ab("Account",S.linked?"linked to SiegeIQ":"not linked");
}
function ab(k,v){
  return "<div class=\"fld\"><label>"+k+"</label><div class=\"h\" style=\"font-size:13px;color:var(--text);margin-top:0\">"
   +esc(v)+"</div></div>";
}

function paintStatus(){
  document.getElementById("ver").textContent="version "+S.app_version;
  var pill=document.getElementById("statepill");
  var d=document.getElementById("statedot");
  d.className="dot "+(S.recording?"on":((S.recorder_paused||S.mode==="off")?"off":"idle"));
  pill.className="state"+(S.recording?" live":"");
  document.getElementById("stateline").textContent=S.status_line||"Idle";
  document.getElementById("pausebtn").textContent=S.recorder_paused?"Resume recording":"Pause recording";
  document.getElementById("syncbtn").textContent=S.sync_paused?"Resume syncing":"Pause syncing";

  // The save buttons cut from the buffer, so with an empty buffer there is
  // nothing to cut. Say which of the several reasons it is empty, because
  // "nothing buffered" on its own is exactly the message that sent somebody
  // looking for a save button that was there all along.
  var grp=document.querySelector(".savegrp"), hint=document.getElementById("savehint");
  if(grp&&hint){
    var why="";
    if(S.mode==="off") why="Recording is switched off, so there is nothing to save yet.";
    else if(S.mode==="manual"&&!S.armed) why="Start recording above first, then this saves the last few minutes.";
    else if(!S.siege_running) why="Nothing to save yet. The buffer fills once Siege is open.";
    else if(!S.buffer_mb) why="Filling the buffer now. Give it a few seconds.";
    // classList, not className. paintStatus runs every couple of seconds, and
    // rewriting the class wholesale re-triggers the entrance animation on every
    // poll - the row would visibly flicker for as long as the window is open.
    grp.classList.toggle("dim",!!why);
    hint.textContent=why;
  }

  var bp=S.buffer_cap_mb?Math.min(100,Math.round(S.buffer_mb/S.buffer_cap_mb*100)):0;
  var cp=S.clips_cap_mb?Math.min(100,Math.round(S.clips_mb/S.clips_cap_mb*100)):0;
  var span=(S.buffer_from&&S.buffer_to)?("holding "+S.buffer_from+" to "+S.buffer_to):"nothing buffered yet";

  document.getElementById("cards").innerHTML=
    card("Recorder",S.recording?"Live":(S.siege_running?"Standing by":"Siege closed"),
         esc(howCapturing()),null,S.recording?"":"sm")
   +card("Buffer",gb(S.buffer_mb),esc(span),bp,"")
   +card("Clips kept",S.clip_count,S.clips_mb+" MB of "+S.clips_cap_mb+" MB",cp,"")
   +card("Account",S.linked?"Linked":"Not linked",
         S.linked?"clips can be sent to SiegeIQ":"pair from the tray to enable uploads",null,"sm");

  var h="";
  if(!S.capture_ready){
    h=note("bad","<b>No capture engine.</b> "+esc(S.capture_problem)+
      " Recording cannot start until this is fixed. Syncing is unaffected.");
  } else if(S.gave_up){
    h=note("bad","<b>Recording is not working on this PC.</b> The recorder tried and stopped, "+
      "rather than retrying forever. Click <b>Run capture test</b> above and it will try every way "+
      "of grabbing your screen and keep the one that works. Takes under a minute.");
  } else if(S.capture_problem){ h=note("warn",esc(S.capture_problem)); }
  else { h=note("","Capture engine ready. "+esc(S.ffmpeg_version)+", encoding with <b>"+esc(S.encoder)+"</b>."); }
  // The arm switch. It only appears in the one mode where it means anything,
  // and it says what will happen rather than naming the state, because "armed"
  // is a word the app invented and nobody else uses.
  if(S.mode==="manual"){
    h+=note(S.armed?"":"warn",
      (S.armed
        ? "<b>Recording is armed.</b> The buffer fills whenever Siege is running, and you can save the last few minutes at any time. "
        : "<b>Nothing is being recorded.</b> You have <b>Only when I arm it</b> selected, so the buffer stays at 0 MB until you switch it on. That is why nothing is there. ")
      +'<button class="primary" style="margin-left:2px;margin-top:8px" onclick="setArm('+(S.armed?"false":"true")+')">'
      +(S.armed?"Stop recording":"Start recording now")+"</button>");
  } else if(S.mode==="siege_running"&&!S.siege_running){
    h+=note("warn","<b>Waiting for Siege.</b> The buffer stays at 0 MB until Rainbow Six Siege is open. "+
      "Nothing is wrong; there is simply nothing to record yet.");
  }
  // A running ffmpeg that is receiving no frames used to be indistinguishable
  // from a working recorder. It is not, and the difference belongs on screen.
  // The one recorder problem with a thirty second fix, said in the place the
  // player is already looking rather than buried in a log.
  if(S.fullscreen){
    h+=note("bad","<b>Siege is in exclusive fullscreen, so nothing can be recorded.</b> "+
      "No screen grab can read a game that has taken the display outright. In Siege, open "+
      "<b>Options</b>, then <b>Display</b>, and set <b>Display Mode</b> to <b>Borderless</b>. "+
      "Recording starts again on its own within a minute of that change, and you do not need to "+
      "touch anything here.");
  }
  if(S.stalled){
    h+=note("bad","<b>Recording is producing nothing.</b> The capture is running but no video is "+
      "reaching the disk. SiegeIQ is switching to window capture on its own. If it keeps happening, "+
      "open Siege, go to <b>Options</b>, <b>Display</b>, and set <b>Display Mode</b> to "+
      "<b>Borderless</b>. The fast GPU grab cannot see a game that has taken the screen in exclusive fullscreen.");
  }
  if(S.audio_note){
    h+=note(S.audio_on?"":"warn",(S.audio_on?"<b>Sound is being recorded.</b> ":"<b>No sound.</b> ")+esc(S.audio_note));
  }
  document.getElementById("engine").innerHTML=h;
  var cn=document.getElementById("clipn");
  if(cn) cn.textContent=S.clip_count?String(S.clip_count):"";
  paintAbout();
}
function setArm(on){
  go("goArm",on?"on":"off").then(function(){ refresh(); });
}
function note(cls,html){
  return '<div class="note '+cls+'"><svg viewBox="0 0 24 24" fill="currentColor">'+
   '<path d="M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zm1 15h-2v-2h2v2zm0-4h-2V7h2v6z"/></svg><div>'+html+'</div></div>';
}
function card(k,v,s,pct,cls){
  return '<div class="card"><div class="k">'+k+'</div><div class="v '+(cls||"")+'">'+esc(v)+'</div>'
   +'<div class="s">'+s+'</div>'
   +((pct===null||pct===undefined)?"":'<div class="bar"><i class="'+(pct>85?"hot":"")+'" style="width:'+pct+'%"></i></div>')
   +'</div>';
}

function paintOptions(){
  document.getElementById("modes").innerHTML=MODES.map(function(o){
    return opt("mode",o[0],o[1],o[2],o[3],settings.mode===o[0]);}).join("");
  document.getElementById("rules").innerHTML=RULES.map(function(o){
    return opt("keep_rule",o[0],o[1],o[2],o[3],settings.keep_rule===o[0]);}).join("");
}
function opt(field,val,title,sub,flag,sel){
  return '<div class="opt'+(sel?" sel":"")+'" onclick="pick(\''+field+'\',\''+val+'\')">'
   +'<div class="rd"></div><div><b>'+title+'</b><span>'+sub+'</span>'
   +(flag?'<span class="flag">'+flag+'</span>':'')+'</div></div>';
}
function pick(f,v){ settings[f]=v; paintOptions(); }

var SND={};
function paintSounds(){
  var el=document.getElementById("soundfields");
  if(!el) return;
  el.innerHTML=
     txt("sound_ok_file","Match uploaded",SND.sound_ok_file,"C:\\Windows\\Media\\Windows Notify System Generic.wav")
    +txt("sound_clip_file","Clip saved",SND.sound_clip_file,"C:\\Windows\\Media\\Windows Balloon.wav")
    +txt("sound_fail_file","Something failed",SND.sound_fail_file,"C:\\Windows\\Media\\Windows Foreground.wav");
}
function txt(k,label,val,placeholder){
  return '<div class="fld"><label>'+label+'</label><input type="text" spellcheck="false" value="'+
   esc(val==null?"":val)+'" placeholder="'+esc(placeholder)+
   '" oninput="SND[\''+k+'\']=this.value"><div class="h">Leave empty for the built-in sound.</div></div>';
}
function saveSounds(){
  go("goSaveSoundPrefs",JSON.stringify(SND)).then(function(err){
    toast(err?err:"Saved. That is what it will sound like.");
  });
}
function clearSounds(){
  SND={sound_ok_file:"",sound_clip_file:"",sound_fail_file:""};
  paintSounds();
  go("goSaveSoundPrefs",JSON.stringify(SND)).then(function(err){
    toast(err?err:"Back to the built-in sounds.");
  });
}

function paintFields(){
  document.getElementById("fields").innerHTML=
    selNum("fps","Frames per second",settings.fps,
        [[30,"30 fps, smallest files"],[60,"60 fps, recommended"],[120,"120 fps"],[144,"144 fps"]],
        "60 suits Siege. 30 roughly halves the file size. The most this build accepts is 240.")
   +selNum("height_cap","Resolution cap",settings.height_cap,
        [[720,"720p, about 1280 x 720"],[1080,"1080p, about 1920 x 1080"],
         [1440,"1440p, about 2560 x 1440"],[2160,"4K, about 3840 x 2160"]],
        "Height only. The width follows your game's shape, so on your 16:10 screen 1440p comes out 2304 x 1440.")
   +sel("encoder","Encoder",settings.encoder,["auto","nvenc","amf","qsv","cpu"],"Auto prefers your graphics card over your processor.")
   +num("quality","Quality",settings.quality,"Lower is better and larger. 23 is a sensible default.")
   +num("max_mbps","Size ceiling (Mbps)",settings.max_mbps,"0 works it out from your resolution and frame rate. Caps how large a busy scene can get.")
   +sel("audio_off","Game sound",settings.audio_off?"off":"on",["on","off"],"Records what your speakers play, if this PC exposes a device that carries it.")
   +selNum("buffer_minutes","Buffer length",settings.buffer_minutes,
        [[15,"15 minutes"],[30,"30 minutes"],[45,"45 minutes, a full ranked match"],
         [60,"1 hour"],[90,"1 hour 30"],[120,"2 hours"]],
        "How far back the Save buttons can reach. A ranked match with overtime runs about 40 minutes.")
   +num("buffer_disk_mb","Buffer limit (MB)",settings.buffer_disk_mb,"A hard cap. It prunes itself and never exceeds this.")
   +num("clip_disk_mb","Clips limit (MB)",settings.clip_disk_mb,"Oldest clips are removed once this is passed.")
   +num("clip_keep_days","Keep clips for (days)",settings.clip_keep_days,"Clips older than this are removed.")
   +num("last_seconds","Last seconds per round",settings.last_seconds,"Used by the last-seconds keep rule.")
   +num("pre_pad_sec","Extra before (seconds)",settings.pre_pad_sec,"Padding at the front, to absorb boundary error.")
   +num("post_pad_sec","Extra after (seconds)",settings.post_pad_sec,"Padding at the end of each clip.")
   +selLabelled("send_to_ai","Send clips to AI Analyze",settings.send_to_ai,
        [["off","Never, keep everything on this PC"],
         ["ask","Ask me each time"],
         ["auto","Send automatically"]],
        "Automatic sending is the same switch as off, so it can be turned back off at any time. "
        +"Nothing is sent while the upload endpoint is not live.");
}
function num(k,label,val,hint){
  return '<div class="fld"><label>'+label+'</label><input type="number" value="'+(val==null?"":val)+
   '" oninput="settings[\''+k+'\']=parseInt(this.value||0,10)"><div class="h">'+hint+'</div></div>';
}
// selLabelled is sel with readable option text. The stored values stay short
// because the app writes them to a config file, but "off", "ask" and "auto" in
// a dropdown make somebody guess what each one does.
// selNum is a dropdown over NUMBERS, with an escape hatch.
//
// These three settings were free-text number boxes, which asked the player to
// know that resolution meant height in pixels and that 45 was a sensible buffer.
// Presets answer both questions by existing. The escape hatch matters just as
// much: a value that is not in the list is added to it as "Custom", so a number
// somebody set deliberately is never silently snapped to the nearest preset.
function selNum(k,label,val,pairs,hint){
  val=Number(val)||0;
  var known=pairs.some(function(p){ return p[0]===val; });
  var all=known?pairs:pairs.concat([[val,"Custom, "+val]]);
  all.sort(function(a,b){ return a[0]-b[0]; });
  var o=all.map(function(p){
    return '<option value="'+p[0]+'"'+(p[0]===val?" selected":"")+'>'+esc(p[1])+'</option>';
  }).join("");
  return '<div class="fld"><label>'+esc(label)+'</label><select onchange="settings[\''+k+'\']=parseInt(this.value,10)">'+o+
   '</select><div class="h">'+esc(hint)+'</div></div>';
}

function selLabelled(k,label,val,pairs,hint){
  var o=pairs.map(function(p){
    return '<option value="'+p[0]+'"'+(p[0]===val?" selected":"")+'>'+esc(p[1])+'</option>';
  }).join("");
  return '<div class="fld"><label>'+esc(label)+'</label><select onchange="settings[\''+k+'\']=this.value">'+o+
   '</select><div class="h">'+esc(hint)+'</div></div>';
}

function sel(k,label,val,opts,hint){
  var o=opts.map(function(x){return '<option value="'+x+'"'+(x===val?" selected":"")+'>'+x+'</option>';}).join("");
  return '<div class="fld"><label>'+label+'</label><select onchange="settings[\''+k+'\']=this.value">'+o+
   '</select><div class="h">'+hint+'</div></div>';
}

function saveSettings(){
  // The select gives back "on" or "off". The stored field is the OFF switch, so
  // an absent key means on for anyone upgrading. See reccfg.go.
  if(typeof settings.audio_off==="string") settings.audio_off=(settings.audio_off==="off");
  delete settings.audio;
  go("goSaveSettings",JSON.stringify(settings)).then(function(err){
    toast(err?err:"Settings saved."); refresh(); });
}
function togglePause(){ go("goTogglePause").then(refresh); }
function toggleSync(){ go("goToggleSync").then(refresh); }
function saveLast(mins){
  mins=Number(mins)||2;
  var label=(mins<1)?(Math.round(mins*60)+" seconds"):(mins+(mins===1?" minute":" minutes"));
  toast("Cutting the last "+label+" from the buffer...",true);
  go("goSaveLast",String(mins)).then(function(err){ toast(err?err:"Clip saved."); refresh(); });
}

function captureTest(){
  var b=document.getElementById("testbtn");
  b.disabled=true; b.textContent="Testing...";
  toast("Trying every way of capturing your screen. This takes under a minute.",true);
  go("goCaptureTest").then(function(err){
    if(err){ b.disabled=false; b.textContent="Run capture test"; toast(err); return; }
    pollTest(0);
  });
}

// The test runs in the background and is polled, rather than being one long
// call that would block every other message to the app while it ran.
function pollTest(n){
  if(n>90){
    document.getElementById("testbtn").disabled=false;
    document.getElementById("testbtn").textContent="Run capture test";
    toast("The test is taking longer than expected. Check the log.");
    return;
  }
  goJ("goCaptureTestState").then(function(st){
    if(!st){ setTimeout(function(){pollTest(n+1);},1000); return; }
    if(st.running){
      if(st.step) toast("Capture test: "+st.step,true);
      setTimeout(function(){pollTest(n+1);},1000);
      return;
    }
    var b=document.getElementById("testbtn");
    b.disabled=false; b.textContent="Run capture test";
    showTestReport(st.report);
  });
}

function showTestReport(r){
    if(!r){ toast("The test did not produce a result."); return; }
    var rows=(r.results||[]).map(function(x){
      var mark=x.ok?'<span style="color:var(--good);font-weight:600">works</span>'
                   :'<span style="color:var(--faint)">'+esc(x.detail||"failed")+'</span>';
      return '<div style="display:flex;gap:12px;padding:7px 0;border-top:1px solid var(--line)">'
        +'<div style="flex:1">'+esc(x.label)+'</div><div>'+mark+'</div></div>';
    }).join("");
    var head=r.applied
      ? note("","<b>Found one that works: "+esc(r.winner)+"</b>. It has been saved, and recording "+
             "will use it from now on. Set <b>When to record</b> back to <b>While Siege is running</b> and try a match.")
      : note("bad","<b>"+esc(r.error||"Nothing worked.")+"</b> Send me the log and I will take it from here.");
    document.getElementById("testout").innerHTML=head
      +'<div style="background:#0E1B29;border:1px solid var(--line);border-radius:10px;padding:6px 15px 12px;margin-top:10px">'
      +'<div style="color:var(--faint);font-size:11px;text-transform:uppercase;letter-spacing:1px;'
      +'font-weight:600;padding:10px 0 2px">What was tried</div>'+rows+'</div>';
    toast(r.applied?"Capture settings updated.":"No configuration worked.");
    refresh();
}

function paintClips(){
  var el=document.getElementById("clips");
  if(!C.length){
    el.className="";
    el.innerHTML='<div class="empty"><svg viewBox="0 0 24 24"><path d="M18 4v1h-2V4H8v1H6V4H4v16h2v-1h2v1h8v-1h2v1h2V4h-2zM8 17H6v-2h2v2zm0-4H6v-2h2v2zm0-4H6V7h2v2zm10 8h-2v-2h2v2zm0-4h-2v-2h2v2zm0-4h-2V7h2v2z"/></svg>'
      +'<p>No clips yet.</p><p class="small">Play a match with the recorder running and they will appear here about a minute after it ends.</p></div>';
    return;
  }
  el.className="clips";
  el.innerHTML=C.map(function(c,i){
    var shot=c.thumb
      ? '<img src="'+c.thumb+'" alt="">'
      : '<div class="none">no preview</div>';
    var head=(c.round?("Round "+c.round):"Clip")+(c.reason?(" - "+c.reason):"");
    return '<div class="clip"><div class="shot" onclick="play('+i+')">'+shot
     +'<div class="ov"><div class="playbtn"><svg viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg></div></div>'
     +'<div class="len">'+dur(c.duration_sec)+'</div></div>'
     +'<div class="cb"><div class="ct">'+esc(head)+'</div>'
     +'<div class="cm">'+esc(c.created)+' &middot; '+mb(c.size_mb)+' MB</div>'
     +(c.estimated?'<span class="tag">start estimated</span>':'')
     +sendLine(c.path)
     +'</div><div class="acts">'
     +'<button onclick="event.stopPropagation();play('+i+')">Play</button>'
     +(S.clip_upload_live?('<button title="Send this clip to AI Analyze" onclick="event.stopPropagation();send('+i+')">Send</button>'):'')
     +'<button class="danger" onclick="event.stopPropagation();del('+i+')">Delete</button>'
     +'</div></div>';
  }).join("");
}
function play(i){ go("goPlayClip",C[i].path).then(function(e){ if(e) toast(e); }); }
// send reports the size, because "Uploading..." on a 283 MB file with no other
// sign of life is indistinguishable from nothing happening, which is exactly
// how somebody ends up asking whether it worked.
// send starts the job and returns. Progress arrives through the normal status
// poll, because the app no longer does this work on the thread that draws the
// window - doing so froze the whole application for five minutes.
function send(i){
  var c=C[i]||{};
  go("goSendClip",c.path).then(function(e){
    if(e){ toast(e); return; }
    toast("Started. "+mb(c.size_mb)+" MB is being made smaller, then sent.");
    refresh();
  });
}

// sendStageFor finds the live job for a clip, if there is one.
function sendStageFor(path){
  var list=(S&&S.sending)||[];
  for(var i=0;i<list.length;i++){ if(list[i].path===path) return list[i]; }
  return null;
}
function sendLine(path){
  var j=sendStageFor(path);
  if(!j) return "";
  var t=j.seconds?(" &middot; "+j.seconds+"s"):"";
  if(j.stage==="compressing") return '<div class="sending">Making a smaller copy to send'+t+"</div>";
  if(j.stage==="uploading")   return '<div class="sending">Uploading to SiegeIQ'+t+"</div>";
  if(j.stage==="done")        return '<div class="sending ok">Sent to SiegeIQ</div>';
  if(j.stage==="failed")      return '<div class="sending bad">'+esc(j.note||"That send failed")+"</div>";
  return "";
}
function del(i){ go("goDeleteClip",C[i].path).then(function(e){
  if(e){ toast(e); } else { toast("Clip deleted."); refresh(); } }); }

// The window is built hidden and appears only when this fires - see
// revealAppWindow. Two frames of grace so the dark page and its first paint are
// genuinely on the surface before it is shown; announcing readiness one frame
// too early puts a white flash back exactly where it was.
function announcePainted(){
  requestAnimationFrame(function(){
    requestAnimationFrame(function(){ go("goReady"); });
  });
}

whenReady(function(){
  refresh();
  announcePainted();
  setInterval(function(){ goJ("goStatus").then(function(s){ if(s){S=s;paintStatus();} }); },2500);
});
</script>
</body>
</html>`
