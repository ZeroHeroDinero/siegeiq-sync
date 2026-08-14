// coachavatar.go - the four coaches, lifted verbatim from siegeiq.gg.
//
// GENERATED, NOT WRITTEN. Extracted from the COACHES object in siegeiq_app.js
// and the <svg id="coach-svg"> block in index.html, so the coach in the desktop
// app is the same character as on the website, down to the last path. The first
// version of this file tinted ONE drawing four ways, which looked plausible in
// isolation and wrong the moment anyone compared it to the site: Orion is a
// robot, Vera and Nova are different people. Four drawings, not four filters.
//
// The website works the same way - Cipher is the drawing sitting in the page,
// and choosing another coach replaces its contents. That is why cipherSVG is a
// whole <svg> element and the other three are only its inner parts.
//
// To refresh after changing a coach on the site, re-run the extractor.
//
// REFRESHED 2026-08-13. Vera and Nova were redrawn because they shared 62 percent of
// their geometry with every colour stripped out: same face ellipse, same body paths, two
// palettes. Vera is now narrow-faced and lean with glasses and a ponytail; Nova is
// round-faced and broad with a beanie and a boom mic. Orion moved from #18AED1 to gold
// (#E8B84B) in the same pass, because he and Cipher shared an accent and the new per-coach
// stage tint would have lit two of the four stages identically.
package main

// coachDisplayNames are the names the WEBSITE shows. "echo" is the key the
// backend stores; "ORION" is what a player sees. Keeping the app's labels equal
// to the site's is the whole point - a player who picks Orion here and sees
// Echo there would reasonably think they had changed something else.
var coachDisplayNames = map[string]string{
	"cipher": "Cipher",
	"echo":   "Orion",
	"vera":   "Vera",
	"nova":   "Nova",
}

const cipherSVG = `<svg id="coach-svg" viewBox="0 0 200 300" width="118" style="display:block;margin:0 auto;overflow:visible" xmlns="http://www.w3.org/2000/svg">
              <ellipse cx="100" cy="289" rx="60" ry="10" fill="#18AED1" opacity="0.10"/>
              <ellipse cx="100" cy="289" rx="38" ry="6" fill="#18AED1" opacity="0.18"/>
              <g id="ti-legL">
                <path d="M84 196 L80 262 Q80 270 89 270 L98 270 L99 198 Z" fill="#242F38" stroke="#0A1519" stroke-width="2"/>
                <path d="M83 236 Q90 240 98 236" stroke="#0A1519" stroke-width="1.4" opacity="0.5"/>
                <path d="M79 264 L77 290 L101 290 L99 264 Z" fill="#161F26" stroke="#0A1519" stroke-width="2"/>
                <rect x="74" y="287" width="28" height="6" rx="2" fill="#0A1015"/>
              </g>
              <g id="ti-legR">
                <path d="M116 196 L120 262 Q120 270 111 270 L102 270 L101 198 Z" fill="#1E2831" stroke="#0A1519" stroke-width="2"/>
                <path d="M117 236 Q110 240 102 236" stroke="#0A1519" stroke-width="1.4" opacity="0.5"/>
                <path d="M121 264 L123 290 L99 290 L101 264 Z" fill="#121A21" stroke="#0A1519" stroke-width="2"/>
                <rect x="98" y="287" width="28" height="6" rx="2" fill="#0A1015"/>
              </g>
              <path d="M80 186 L120 186 L123 198 L77 198 Z" fill="#161F26" stroke="#0A1519" stroke-width="2"/>
              <g id="ti-torso">
                <path d="M68 106 Q64 98 80 96 L120 96 Q136 98 132 106 L134 170 Q134 190 100 190 Q66 190 66 170 Z" fill="#2B3843" stroke="#0A1519" stroke-width="2"/>
                <path d="M84 96 L100 120 L116 96 L110 96 L100 110 L90 96 Z" fill="#1B2731"/>
                <path d="M100 100 L100 188" stroke="#1B2731" stroke-width="2.4"/>
                <path d="M78 112 Q76 150 82 182" stroke="#22303A" stroke-width="2" fill="none"/>
                <path d="M122 112 Q124 150 118 182" stroke="#22303A" stroke-width="2" fill="none"/>
                <path d="M72 104 Q84 100 92 104" stroke="#18AED1" stroke-width="2.4" fill="none" opacity="0.75"/>
                <path d="M128 104 Q116 100 108 104" stroke="#18AED1" stroke-width="2.4" fill="none" opacity="0.55"/>
                <rect x="106" y="118" width="20" height="15" rx="2" fill="#18AED1"/>
                <text x="116" y="129.5" text-anchor="middle" font-family="Arial,sans-serif" font-size="9" font-weight="800" fill="#07131B">IQ</text>
                <rect x="74" y="120" width="16" height="4" rx="2" fill="#3A4854"/>
                <rect x="74" y="128" width="11" height="4" rx="2" fill="#3A4854"/>
              </g>
              <g id="ti-arms">
                <path d="M74 108 L58 154" stroke="#0A1519" stroke-width="24" stroke-linecap="round"/>
                <path d="M58 154 L72 188" stroke="#0A1519" stroke-width="20" stroke-linecap="round"/>
                <path d="M126 108 L142 154" stroke="#0A1519" stroke-width="24" stroke-linecap="round"/>
                <path d="M142 154 L128 188" stroke="#0A1519" stroke-width="20" stroke-linecap="round"/>
                <path d="M74 108 L58 154" stroke="#2B3843" stroke-width="19" stroke-linecap="round"/>
                <path d="M58 154 L72 188" stroke="#22303A" stroke-width="15" stroke-linecap="round"/>
                <path d="M126 108 L142 154" stroke="#22303A" stroke-width="19" stroke-linecap="round"/>
                <path d="M142 154 L128 188" stroke="#1E2831" stroke-width="15" stroke-linecap="round"/>
                <path d="M62 118 L52 148" stroke="#18AED1" stroke-width="2.4" stroke-linecap="round" opacity="0.5"/>
                <circle cx="72" cy="190" r="9" fill="#E8B98C" stroke="#0A1519" stroke-width="1.8"/>
                <circle cx="128" cy="190" r="9" fill="#E0AE80" stroke="#0A1519" stroke-width="1.8"/>
                <g opacity="0.95"><rect x="118" y="158" width="38" height="27" rx="4" fill="#0E1B22" stroke="#18AED1" stroke-width="1.3"/><path d="M124 166 L148 166 M124 172 L144 172 M124 178 L150 178" stroke="#18AED1" stroke-width="1.5" opacity="0.8"/></g>
              </g>
              <g id="ti-head">
                <rect x="92" y="86" width="16" height="13" rx="4" fill="#DBA97E"/>
                <path d="M76 60 Q76 42 100 42 Q124 42 124 60 Q124 80 116 88 Q108 96 100 96 Q92 96 84 88 Q76 80 76 60 Z" fill="#E8B98C" stroke="#0A1519" stroke-width="1.7"/>
                <path d="M80 74 Q82 92 100 96 Q118 92 120 74 Q116 86 100 88 Q84 86 80 74 Z" fill="#5B6570" opacity="0.55"/>
                <path d="M84 88 Q100 98 116 88 Q108 94 100 94 Q92 94 84 88 Z" fill="#4C5660" opacity="0.6"/>
                <circle cx="77" cy="66" r="4.5" fill="#DBA97E" stroke="#0A1519" stroke-width="1.2"/>
                <path d="M74 60 Q70 34 100 32 Q130 34 126 60 Q126 50 118 46 Q126 42 112 40 Q120 38 100 38 Q80 38 84 44 Q74 48 78 58 Z" fill="#8B98A2" stroke="#0A1519" stroke-width="1.6"/>
                <path d="M84 44 Q100 38 116 44 Q104 41 100 41 Q90 41 84 44 Z" fill="#B4BEC6" opacity="0.5"/>
                <path d="M80 42 Q78 52 80 60" stroke="#AEB8C0" stroke-width="1.4" fill="none" opacity="0.6"/>
                <path d="M82 62 Q89 60 95 62" fill="none" stroke="#5B6570" stroke-width="2.4" stroke-linecap="round"/>
                <path d="M105 62 Q111 60 118 62" fill="none" stroke="#5B6570" stroke-width="2.4" stroke-linecap="round"/>
                <g id="ax-eye">
                  <ellipse cx="90" cy="69" rx="6" ry="4.4" fill="#FFFFFF" stroke="#0A1519" stroke-width="1.1"/>
                  <ellipse cx="110" cy="69" rx="6" ry="4.4" fill="#FFFFFF" stroke="#0A1519" stroke-width="1.1"/>
                  <circle cx="91" cy="69.5" r="2.6" fill="#3A5B63"/>
                  <circle cx="109" cy="69.5" r="2.6" fill="#3A5B63"/>
                  <circle cx="91" cy="69.5" r="1.2" fill="#0A1519"/>
                  <circle cx="109" cy="69.5" r="1.2" fill="#0A1519"/>
                  <circle cx="89.8" cy="68.4" r="0.7" fill="#FFFFFF"/>
                  <circle cx="107.8" cy="68.4" r="0.7" fill="#FFFFFF"/>
                </g>
                <path d="M84 56 L86 66" stroke="#C98F63" stroke-width="1.2" opacity="0.7"/>
                <path d="M100 70 Q98 78 96 80 Q100 82 104 80" fill="none" stroke="#C98F63" stroke-width="1.3" stroke-linecap="round"/>
                <g id="coach-mouth">
                  <path d="M92 85 Q100 89 108 85 Q104 91 100 91 Q96 91 92 85 Z" fill="#7A3A38"/>
                  <path d="M93 85.5 Q100 88 107 85.5" fill="none" stroke="#E8B98C" stroke-width="1.2"/>
                </g>
                <path d="M76 52 Q100 30 124 52" fill="none" stroke="#161F26" stroke-width="4" stroke-linecap="round"/>
                <rect x="120" y="60" width="12" height="18" rx="5" fill="#1B2731" stroke="#0A1519" stroke-width="1.6"/>
                <circle id="cf-led" cx="126" cy="69" r="2.4" fill="#18AED1"/>
                <path d="M120 74 Q108 84 101 86" fill="none" stroke="#161F26" stroke-width="2.6" stroke-linecap="round"/>
                <circle cx="100" cy="86" r="2.6" fill="#1B2731" stroke="#0A1519" stroke-width="1"/>
              </g>
            </svg>`

// coachInner replaces the contents of the Cipher drawing, exactly as the site does.
var coachInner = map[string]string{
	"echo": `<ellipse cx="100" cy="289" rx="60" ry="11" fill="#E8B84B" opacity="0.10"/><ellipse cx="100" cy="289" rx="40" ry="6.5" fill="#E8B84B" opacity="0.18"/><g id="op-body"><path d="M82 198 L72 280 L97 280 L99 200 Z" fill="#2C323C" stroke="#11141A" stroke-width="2"/><path d="M118 198 L128 280 L103 280 L101 200 Z" fill="#262B34" stroke="#11141A" stroke-width="2"/><rect x="73" y="231" width="23" height="19" rx="6" fill="#3C4450" stroke="#11141A" stroke-width="1.5"/><rect x="104" y="231" width="23" height="19" rx="6" fill="#343B45" stroke="#11141A" stroke-width="1.5"/><path d="M70 280 L64 297 L98 297 L96 280 Z" fill="#1A1D24" stroke="#11141A" stroke-width="2"/><path d="M130 280 L136 297 L102 297 L104 280 Z" fill="#1A1D24" stroke="#11141A" stroke-width="2"/><rect x="62" y="295" width="36" height="5" rx="2" fill="#0E1116"/><rect x="102" y="295" width="36" height="5" rx="2" fill="#0E1116"/><rect x="74" y="183" width="52" height="16" rx="3" fill="#1A1D24" stroke="#11141A" stroke-width="2"/><rect x="93" y="185" width="14" height="12" rx="2" fill="#E8B84B"/><path d="M70 104 L46 150" stroke="#11141A" stroke-width="28" stroke-linecap="round"/><path d="M46 150 L80 188" stroke="#11141A" stroke-width="24" stroke-linecap="round"/><path d="M130 104 L154 150" stroke="#11141A" stroke-width="28" stroke-linecap="round"/><path d="M154 150 L120 188" stroke="#11141A" stroke-width="24" stroke-linecap="round"/><path d="M70 104 L46 150" stroke="#262B34" stroke-width="24" stroke-linecap="round"/><path d="M46 150 L80 188" stroke="#2C323C" stroke-width="20" stroke-linecap="round"/><path d="M130 104 L154 150" stroke="#2C323C" stroke-width="24" stroke-linecap="round"/><path d="M154 150 L120 188" stroke="#262B34" stroke-width="20" stroke-linecap="round"/><path d="M66 110 L48 144" stroke="#3C4450" stroke-width="8" stroke-linecap="round" opacity="0.55"/><path d="M134 110 L152 144" stroke="#4A525C" stroke-width="8" stroke-linecap="round" opacity="0.55"/><path d="M64 96 Q58 89 74 87 L126 87 Q142 89 136 96 L138 165 Q138 189 100 189 Q62 189 62 165 Z" fill="#2C323C" stroke="#11141A" stroke-width="2"/><rect x="80" y="90" width="13" height="68" rx="4" fill="#343B45"/><rect x="107" y="90" width="13" height="68" rx="4" fill="#262B34"/><rect x="70" y="150" width="20" height="24" rx="3" fill="#343B45" stroke="#11141A" stroke-width="1.5"/><rect x="110" y="150" width="20" height="24" rx="3" fill="#2C323C" stroke="#11141A" stroke-width="1.5"/><line x1="70" y1="132" x2="130" y2="132" stroke="#11141A" stroke-width="1" opacity="0.5"/><rect x="86" y="103" width="28" height="20" rx="3" fill="#E8B84B"/><text x="100" y="118" text-anchor="middle" font-family="Arial,sans-serif" font-size="13" font-weight="800" fill="#1A1D24">IQ</text><circle cx="81" cy="187" r="11" fill="#1F2229" stroke="#11141A" stroke-width="2"/><circle cx="119" cy="187" r="11" fill="#1F2229" stroke="#11141A" stroke-width="2"/><rect x="89" y="78" width="22" height="16" rx="3" fill="#1F2229"/><path d="M75 50 Q75 23 100 23 Q125 23 125 50 L125 63 Q125 80 100 80 Q75 80 75 63 Z" fill="#2C323C" stroke="#11141A" stroke-width="2"/><path d="M80 42 Q85 29 100 29 Q115 29 120 42" fill="none" stroke="#525C6A" stroke-width="2" opacity="0.7"/><path d="M80 60 Q80 79 100 79 Q120 79 120 60 L120 55 L80 55 Z" fill="#1A1D24"/><rect x="78" y="48" width="44" height="13" rx="4" fill="#0E1116" stroke="#11141A" stroke-width="1.5"/><rect id="coach-mouth" x="82" y="52.5" width="36" height="4.5" rx="2" fill="#F5D98A" style="transform-box:fill-box;transform-origin:center;transform:scaleY(0.5)"/><path d="M74 55 Q70 36 88 29" fill="none" stroke="#1A1D24" stroke-width="2.5"/><circle cx="124" cy="57" r="8" fill="#1F2229" stroke="#11141A" stroke-width="2"/><circle cx="124" cy="57" r="3.2" fill="#E8B84B"/><path d="M124 65 Q114 73 104 73" fill="none" stroke="#1A1D24" stroke-width="2.5"/><circle cx="103" cy="73" r="2.8" fill="#E8B84B"/></g>`,
	"vera": `<ellipse cx="100" cy="291" rx="46" ry="9" fill="#22D3EE" opacity="0.10"/><ellipse cx="100" cy="291" rx="28" ry="5" fill="#22D3EE" opacity="0.20"/><g id="ti-legL"><path d="M86 190 L81 268 Q81 274 88 274 L96 274 L98 192 Z" fill="#2A303A" stroke="#11141A" stroke-width="2"/><path d="M84 206 L96 206" stroke="#22D3EE" stroke-width="2" opacity="0.55"/><path d="M80 272 L78 294 L99 294 L98 272 Z" fill="#1A1D24" stroke="#11141A" stroke-width="2"/><rect x="76" y="291" width="24" height="5" rx="2" fill="#0E1116"/></g><g id="ti-legR"><path d="M114 190 L119 268 Q119 274 112 274 L104 274 L102 192 Z" fill="#242932" stroke="#11141A" stroke-width="2"/><path d="M116 206 L104 206" stroke="#22D3EE" stroke-width="2" opacity="0.55"/><path d="M120 272 L122 294 L101 294 L102 272 Z" fill="#1A1D24" stroke="#11141A" stroke-width="2"/><rect x="100" y="291" width="24" height="5" rx="2" fill="#0E1116"/></g><rect x="80" y="180" width="40" height="12" rx="3" fill="#1A1D24" stroke="#11141A" stroke-width="2"/><rect x="95" y="182" width="10" height="8" rx="1" fill="#22D3EE"/><g id="ti-arms"><path d="M80 112 L69 154" stroke="#2A303A" stroke-width="17" stroke-linecap="round"/><path d="M120 112 L131 154" stroke="#242932" stroke-width="17" stroke-linecap="round"/><path d="M80 120 L71 150" stroke="#22D3EE" stroke-width="2.4" stroke-linecap="round" opacity="0.6"/><path d="M120 120 L129 150" stroke="#22D3EE" stroke-width="2.4" stroke-linecap="round" opacity="0.6"/><circle cx="69" cy="157" r="8" fill="#E8B083" stroke="#11141A" stroke-width="1.5"/><circle cx="131" cy="157" r="8" fill="#E8B083" stroke="#11141A" stroke-width="1.5"/><g transform="rotate(-8 132 164)"><rect x="122" y="151" width="21" height="26" rx="3" fill="#151A21" stroke="#22D3EE" stroke-width="1.6"/><path d="M126 159 L139 159 M126 164 L136 164 M126 169 L140 169" stroke="#22D3EE" stroke-width="1.4" opacity="0.75"/></g></g><g id="ti-torso"><path d="M78 104 Q75 98 85 96 L115 96 Q125 98 122 104 L124 164 Q124 184 100 184 Q76 184 76 164 Z" fill="#2A303A" stroke="#11141A" stroke-width="2"/><path d="M88 96 Q100 110 112 96 L112 91 L88 91 Z" fill="#1A1D24"/><path d="M92 104 Q90 132 94 152" stroke="#22D3EE" stroke-width="2" fill="none" stroke-linecap="round"/><path d="M108 104 Q110 132 106 152" stroke="#22D3EE" stroke-width="2" fill="none" stroke-linecap="round"/><rect x="88" y="120" width="24" height="17" rx="3" fill="#22D3EE"/><text x="100" y="133.5" text-anchor="middle" font-family="Arial,sans-serif" font-size="11" font-weight="800" fill="#0E1116">IQ</text></g><g id="ti-head"><rect x="94" y="79" width="12" height="15" rx="4" fill="#D69A6E"/><path d="M117 42 Q137 56 133 86 Q130 106 120 114 Q127 92 123 74 Q120 57 111 48 Z" fill="#332417" stroke="#11141A" stroke-width="1.6"/><ellipse cx="100" cy="58" rx="17.5" ry="24.5" fill="#E8B083" stroke="#11141A" stroke-width="1.4"/><path d="M82 50 Q79 24 100 22 Q121 24 118 50 Q114 36 100 36 Q86 36 82 50 Z" fill="#3E2C1D" stroke="#11141A" stroke-width="1.3"/><path d="M84 40 Q93 30 107 32" fill="none" stroke="#57402C" stroke-width="1.6" opacity="0.75"/><ellipse cx="92.5" cy="58" rx="3.6" ry="2.9" fill="#FFFFFF" stroke="#11141A" stroke-width="0.7"/><ellipse cx="107.5" cy="58" rx="3.6" ry="2.9" fill="#FFFFFF" stroke="#11141A" stroke-width="0.7"/><circle cx="93" cy="58.4" r="2.1" fill="#1C7C90"/><circle cx="108" cy="58.4" r="2.1" fill="#1C7C90"/><circle cx="93" cy="58.4" r="0.9" fill="#0E1116"/><circle cx="108" cy="58.4" r="0.9" fill="#0E1116"/><rect x="85" y="52.5" width="15.5" height="11" rx="3.5" fill="#22D3EE" opacity="0.12"/><rect x="99.5" y="52.5" width="15.5" height="11" rx="3.5" fill="#22D3EE" opacity="0.12"/><rect x="85" y="52.5" width="15.5" height="11" rx="3.5" fill="none" stroke="#22D3EE" stroke-width="1.8"/><rect x="99.5" y="52.5" width="15.5" height="11" rx="3.5" fill="none" stroke="#22D3EE" stroke-width="1.8"/><path d="M100.5 57.5 L99.5 57.5" stroke="#22D3EE" stroke-width="1.8"/><path d="M85 56 L79.5 55" stroke="#22D3EE" stroke-width="1.6" stroke-linecap="round"/><path d="M115 56 L120.5 55" stroke="#22D3EE" stroke-width="1.6" stroke-linecap="round"/><path d="M100 63 Q99 67 98 69 Q100 70 102 69" fill="none" stroke="#C98F63" stroke-width="1" stroke-linecap="round"/><g id="coach-mouth" style="transform-box:fill-box;transform-origin:center;transform:scaleY(0.5)"><path d="M94 74 Q100 71.5 106 74 Q100 79 94 74 Z" fill="#B65C63"/></g><path d="M117 55 Q123 59 121 67" fill="none" stroke="#22D3EE" stroke-width="1.8" stroke-linecap="round"/><circle cx="121" cy="68" r="2.1" fill="#22D3EE"/></g>`,
	"nova": `<ellipse cx="100" cy="291" rx="54" ry="10" fill="#F43F8E" opacity="0.10"/><ellipse cx="100" cy="291" rx="33" ry="6" fill="#F43F8E" opacity="0.20"/><g id="ti-legL"><path d="M80 198 L70 266 Q70 273 80 273 L94 273 L96 200 Z" fill="#2E2A35" stroke="#11141A" stroke-width="2"/><path d="M77 220 L93 220" stroke="#F43F8E" stroke-width="2.4" opacity="0.55"/><path d="M69 271 L66 287 L97 287 L96 271 Z" fill="#26222C" stroke="#11141A" stroke-width="2"/><rect x="62" y="284" width="37" height="10" rx="5" fill="#F2F0EC" stroke="#11141A" stroke-width="1.4"/><path d="M66 289 L95 289" stroke="#F43F8E" stroke-width="1.8" opacity="0.7"/></g><g id="ti-legR"><path d="M120 198 L130 266 Q130 273 120 273 L106 273 L104 200 Z" fill="#282430" stroke="#11141A" stroke-width="2"/><path d="M123 220 L107 220" stroke="#F43F8E" stroke-width="2.4" opacity="0.55"/><path d="M131 271 L134 287 L103 287 L104 271 Z" fill="#26222C" stroke="#11141A" stroke-width="2"/><rect x="101" y="284" width="37" height="10" rx="5" fill="#F2F0EC" stroke="#11141A" stroke-width="1.4"/><path d="M105 289 L134 289" stroke="#F43F8E" stroke-width="1.8" opacity="0.7"/></g><rect x="72" y="188" width="56" height="14" rx="4" fill="#26222C" stroke="#11141A" stroke-width="2"/><rect x="93" y="191" width="14" height="8" rx="2" fill="#F43F8E"/><g id="ti-torso"><path d="M67 106 Q62 97 76 95 L124 95 Q138 97 133 106 L136 172 Q136 195 100 195 Q64 195 64 172 Z" fill="#2E2A35" stroke="#11141A" stroke-width="2"/><path d="M65 126 L135 126 M65 148 L135 148 M65 170 L135 170" stroke="#11141A" stroke-width="1.5" opacity="0.4"/><path d="M82 95 Q100 116 118 95 L118 89 L82 89 Z" fill="#26222C"/><path d="M92 100 L91 120" stroke="#F43F8E" stroke-width="2.6" stroke-linecap="round"/><path d="M108 100 L109 120" stroke="#F43F8E" stroke-width="2.6" stroke-linecap="round"/><rect x="86" y="132" width="28" height="20" rx="3" fill="#F43F8E"/><text x="100" y="147" text-anchor="middle" font-family="Arial,sans-serif" font-size="13" font-weight="800" fill="#160E12">IQ</text></g><g id="ti-arms"><path d="M73 110 L51 158" stroke="#2E2A35" stroke-width="25" stroke-linecap="round"/><path d="M127 110 L149 158" stroke="#282430" stroke-width="25" stroke-linecap="round"/><path d="M71 122 L54 152" stroke="#F43F8E" stroke-width="3.2" stroke-linecap="round" opacity="0.65"/><path d="M129 122 L146 152" stroke="#F43F8E" stroke-width="3.2" stroke-linecap="round" opacity="0.65"/><circle cx="51" cy="163" r="11" fill="#D69A6E" stroke="#11141A" stroke-width="1.6"/><circle cx="149" cy="163" r="11" fill="#E8B083" stroke="#11141A" stroke-width="1.6"/></g><g id="ti-head"><rect x="93" y="82" width="14" height="12" rx="4" fill="#D69A6E"/><path d="M72 54 Q59 86 70 124 Q77 98 79 76 Z" fill="#2C2233" stroke="#11141A" stroke-width="1.6"/><path d="M128 54 Q143 88 130 128 Q123 100 121 76 Z" fill="#2C2233" stroke="#11141A" stroke-width="1.6"/><path d="M134 72 Q141 98 133 122" stroke="#F43F8E" stroke-width="2" fill="none" opacity="0.55"/><path d="M66 72 Q59 98 67 120" stroke="#F43F8E" stroke-width="2" fill="none" opacity="0.45"/><path d="M77 54 Q100 34 123 54" fill="none" stroke="#F43F8E" stroke-width="3.6" stroke-linecap="round"/><ellipse cx="100" cy="66" rx="22.5" ry="20.5" fill="#E8B083" stroke="#11141A" stroke-width="1.5"/><path d="M78 60 Q76 42 100 40 Q124 42 122 60 Q118 50 100 50 Q82 50 78 60 Z" fill="#2C2233" stroke="#11141A" stroke-width="1.3"/><path d="M76 48 Q76 21 100 21 Q124 21 124 48 Z" fill="#3A2C42" stroke="#11141A" stroke-width="2"/><rect x="73" y="43" width="54" height="10" rx="4" fill="#4A3854" stroke="#11141A" stroke-width="1.6"/><circle cx="100" cy="19" r="6.5" fill="#F43F8E" stroke="#11141A" stroke-width="1.4"/><ellipse cx="91" cy="66" rx="4.2" ry="4" fill="#FFFFFF" stroke="#11141A" stroke-width="0.7"/><ellipse cx="109" cy="66" rx="4.2" ry="4" fill="#FFFFFF" stroke="#11141A" stroke-width="0.7"/><circle cx="91.5" cy="66.4" r="2.5" fill="#4A2B3A"/><circle cx="109.5" cy="66.4" r="2.5" fill="#4A2B3A"/><circle cx="90.5" cy="65.2" r="0.9" fill="#FFFFFF"/><circle cx="108.5" cy="65.2" r="0.9" fill="#FFFFFF"/><path d="M85 59 Q91 55 97 58.5 M103 58.5 Q109 55 115 59" fill="none" stroke="#241A20" stroke-width="1.9" stroke-linecap="round"/><ellipse cx="83" cy="74" rx="4.2" ry="2.3" fill="#F43F8E" opacity="0.28"/><ellipse cx="117" cy="74" rx="4.2" ry="2.3" fill="#F43F8E" opacity="0.28"/><g id="coach-mouth" style="transform-box:fill-box;transform-origin:center;transform:scaleY(0.5)"><ellipse cx="100" cy="78" rx="7.5" ry="5.5" fill="#7A3A3E"/><rect x="94.5" y="74.5" width="11" height="2.6" rx="1" fill="#EDE6DA"/></g><ellipse cx="76" cy="64" rx="7.5" ry="10" fill="#3A2C42" stroke="#11141A" stroke-width="2"/><ellipse cx="124" cy="64" rx="7.5" ry="10" fill="#3A2C42" stroke="#11141A" stroke-width="2"/><circle cx="76" cy="64" r="3" fill="#F43F8E"/><circle cx="124" cy="64" r="3" fill="#F43F8E"/><path d="M76 74 Q80 91 94 88" fill="none" stroke="#2C2233" stroke-width="3.4" stroke-linecap="round"/><ellipse cx="95" cy="88" rx="4.2" ry="3.1" fill="#F43F8E"/></g>`,
}
