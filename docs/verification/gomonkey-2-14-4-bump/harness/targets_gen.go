// Code generated for the review harness of PR #6228. Provides >256 distinct
// patch targets to probe gomonkey 2.14.4's amd64 trampoline pool (256 slots).
package harness

//go:noinline
func capTarget000() int { return 0 }

//go:noinline
func capTarget001() int { return 1 }

//go:noinline
func capTarget002() int { return 2 }

//go:noinline
func capTarget003() int { return 3 }

//go:noinline
func capTarget004() int { return 4 }

//go:noinline
func capTarget005() int { return 5 }

//go:noinline
func capTarget006() int { return 6 }

//go:noinline
func capTarget007() int { return 7 }

//go:noinline
func capTarget008() int { return 8 }

//go:noinline
func capTarget009() int { return 9 }

//go:noinline
func capTarget010() int { return 10 }

//go:noinline
func capTarget011() int { return 11 }

//go:noinline
func capTarget012() int { return 12 }

//go:noinline
func capTarget013() int { return 13 }

//go:noinline
func capTarget014() int { return 14 }

//go:noinline
func capTarget015() int { return 15 }

//go:noinline
func capTarget016() int { return 16 }

//go:noinline
func capTarget017() int { return 17 }

//go:noinline
func capTarget018() int { return 18 }

//go:noinline
func capTarget019() int { return 19 }

//go:noinline
func capTarget020() int { return 20 }

//go:noinline
func capTarget021() int { return 21 }

//go:noinline
func capTarget022() int { return 22 }

//go:noinline
func capTarget023() int { return 23 }

//go:noinline
func capTarget024() int { return 24 }

//go:noinline
func capTarget025() int { return 25 }

//go:noinline
func capTarget026() int { return 26 }

//go:noinline
func capTarget027() int { return 27 }

//go:noinline
func capTarget028() int { return 28 }

//go:noinline
func capTarget029() int { return 29 }

//go:noinline
func capTarget030() int { return 30 }

//go:noinline
func capTarget031() int { return 31 }

//go:noinline
func capTarget032() int { return 32 }

//go:noinline
func capTarget033() int { return 33 }

//go:noinline
func capTarget034() int { return 34 }

//go:noinline
func capTarget035() int { return 35 }

//go:noinline
func capTarget036() int { return 36 }

//go:noinline
func capTarget037() int { return 37 }

//go:noinline
func capTarget038() int { return 38 }

//go:noinline
func capTarget039() int { return 39 }

//go:noinline
func capTarget040() int { return 40 }

//go:noinline
func capTarget041() int { return 41 }

//go:noinline
func capTarget042() int { return 42 }

//go:noinline
func capTarget043() int { return 43 }

//go:noinline
func capTarget044() int { return 44 }

//go:noinline
func capTarget045() int { return 45 }

//go:noinline
func capTarget046() int { return 46 }

//go:noinline
func capTarget047() int { return 47 }

//go:noinline
func capTarget048() int { return 48 }

//go:noinline
func capTarget049() int { return 49 }

//go:noinline
func capTarget050() int { return 50 }

//go:noinline
func capTarget051() int { return 51 }

//go:noinline
func capTarget052() int { return 52 }

//go:noinline
func capTarget053() int { return 53 }

//go:noinline
func capTarget054() int { return 54 }

//go:noinline
func capTarget055() int { return 55 }

//go:noinline
func capTarget056() int { return 56 }

//go:noinline
func capTarget057() int { return 57 }

//go:noinline
func capTarget058() int { return 58 }

//go:noinline
func capTarget059() int { return 59 }

//go:noinline
func capTarget060() int { return 60 }

//go:noinline
func capTarget061() int { return 61 }

//go:noinline
func capTarget062() int { return 62 }

//go:noinline
func capTarget063() int { return 63 }

//go:noinline
func capTarget064() int { return 64 }

//go:noinline
func capTarget065() int { return 65 }

//go:noinline
func capTarget066() int { return 66 }

//go:noinline
func capTarget067() int { return 67 }

//go:noinline
func capTarget068() int { return 68 }

//go:noinline
func capTarget069() int { return 69 }

//go:noinline
func capTarget070() int { return 70 }

//go:noinline
func capTarget071() int { return 71 }

//go:noinline
func capTarget072() int { return 72 }

//go:noinline
func capTarget073() int { return 73 }

//go:noinline
func capTarget074() int { return 74 }

//go:noinline
func capTarget075() int { return 75 }

//go:noinline
func capTarget076() int { return 76 }

//go:noinline
func capTarget077() int { return 77 }

//go:noinline
func capTarget078() int { return 78 }

//go:noinline
func capTarget079() int { return 79 }

//go:noinline
func capTarget080() int { return 80 }

//go:noinline
func capTarget081() int { return 81 }

//go:noinline
func capTarget082() int { return 82 }

//go:noinline
func capTarget083() int { return 83 }

//go:noinline
func capTarget084() int { return 84 }

//go:noinline
func capTarget085() int { return 85 }

//go:noinline
func capTarget086() int { return 86 }

//go:noinline
func capTarget087() int { return 87 }

//go:noinline
func capTarget088() int { return 88 }

//go:noinline
func capTarget089() int { return 89 }

//go:noinline
func capTarget090() int { return 90 }

//go:noinline
func capTarget091() int { return 91 }

//go:noinline
func capTarget092() int { return 92 }

//go:noinline
func capTarget093() int { return 93 }

//go:noinline
func capTarget094() int { return 94 }

//go:noinline
func capTarget095() int { return 95 }

//go:noinline
func capTarget096() int { return 96 }

//go:noinline
func capTarget097() int { return 97 }

//go:noinline
func capTarget098() int { return 98 }

//go:noinline
func capTarget099() int { return 99 }

//go:noinline
func capTarget100() int { return 100 }

//go:noinline
func capTarget101() int { return 101 }

//go:noinline
func capTarget102() int { return 102 }

//go:noinline
func capTarget103() int { return 103 }

//go:noinline
func capTarget104() int { return 104 }

//go:noinline
func capTarget105() int { return 105 }

//go:noinline
func capTarget106() int { return 106 }

//go:noinline
func capTarget107() int { return 107 }

//go:noinline
func capTarget108() int { return 108 }

//go:noinline
func capTarget109() int { return 109 }

//go:noinline
func capTarget110() int { return 110 }

//go:noinline
func capTarget111() int { return 111 }

//go:noinline
func capTarget112() int { return 112 }

//go:noinline
func capTarget113() int { return 113 }

//go:noinline
func capTarget114() int { return 114 }

//go:noinline
func capTarget115() int { return 115 }

//go:noinline
func capTarget116() int { return 116 }

//go:noinline
func capTarget117() int { return 117 }

//go:noinline
func capTarget118() int { return 118 }

//go:noinline
func capTarget119() int { return 119 }

//go:noinline
func capTarget120() int { return 120 }

//go:noinline
func capTarget121() int { return 121 }

//go:noinline
func capTarget122() int { return 122 }

//go:noinline
func capTarget123() int { return 123 }

//go:noinline
func capTarget124() int { return 124 }

//go:noinline
func capTarget125() int { return 125 }

//go:noinline
func capTarget126() int { return 126 }

//go:noinline
func capTarget127() int { return 127 }

//go:noinline
func capTarget128() int { return 128 }

//go:noinline
func capTarget129() int { return 129 }

//go:noinline
func capTarget130() int { return 130 }

//go:noinline
func capTarget131() int { return 131 }

//go:noinline
func capTarget132() int { return 132 }

//go:noinline
func capTarget133() int { return 133 }

//go:noinline
func capTarget134() int { return 134 }

//go:noinline
func capTarget135() int { return 135 }

//go:noinline
func capTarget136() int { return 136 }

//go:noinline
func capTarget137() int { return 137 }

//go:noinline
func capTarget138() int { return 138 }

//go:noinline
func capTarget139() int { return 139 }

//go:noinline
func capTarget140() int { return 140 }

//go:noinline
func capTarget141() int { return 141 }

//go:noinline
func capTarget142() int { return 142 }

//go:noinline
func capTarget143() int { return 143 }

//go:noinline
func capTarget144() int { return 144 }

//go:noinline
func capTarget145() int { return 145 }

//go:noinline
func capTarget146() int { return 146 }

//go:noinline
func capTarget147() int { return 147 }

//go:noinline
func capTarget148() int { return 148 }

//go:noinline
func capTarget149() int { return 149 }

//go:noinline
func capTarget150() int { return 150 }

//go:noinline
func capTarget151() int { return 151 }

//go:noinline
func capTarget152() int { return 152 }

//go:noinline
func capTarget153() int { return 153 }

//go:noinline
func capTarget154() int { return 154 }

//go:noinline
func capTarget155() int { return 155 }

//go:noinline
func capTarget156() int { return 156 }

//go:noinline
func capTarget157() int { return 157 }

//go:noinline
func capTarget158() int { return 158 }

//go:noinline
func capTarget159() int { return 159 }

//go:noinline
func capTarget160() int { return 160 }

//go:noinline
func capTarget161() int { return 161 }

//go:noinline
func capTarget162() int { return 162 }

//go:noinline
func capTarget163() int { return 163 }

//go:noinline
func capTarget164() int { return 164 }

//go:noinline
func capTarget165() int { return 165 }

//go:noinline
func capTarget166() int { return 166 }

//go:noinline
func capTarget167() int { return 167 }

//go:noinline
func capTarget168() int { return 168 }

//go:noinline
func capTarget169() int { return 169 }

//go:noinline
func capTarget170() int { return 170 }

//go:noinline
func capTarget171() int { return 171 }

//go:noinline
func capTarget172() int { return 172 }

//go:noinline
func capTarget173() int { return 173 }

//go:noinline
func capTarget174() int { return 174 }

//go:noinline
func capTarget175() int { return 175 }

//go:noinline
func capTarget176() int { return 176 }

//go:noinline
func capTarget177() int { return 177 }

//go:noinline
func capTarget178() int { return 178 }

//go:noinline
func capTarget179() int { return 179 }

//go:noinline
func capTarget180() int { return 180 }

//go:noinline
func capTarget181() int { return 181 }

//go:noinline
func capTarget182() int { return 182 }

//go:noinline
func capTarget183() int { return 183 }

//go:noinline
func capTarget184() int { return 184 }

//go:noinline
func capTarget185() int { return 185 }

//go:noinline
func capTarget186() int { return 186 }

//go:noinline
func capTarget187() int { return 187 }

//go:noinline
func capTarget188() int { return 188 }

//go:noinline
func capTarget189() int { return 189 }

//go:noinline
func capTarget190() int { return 190 }

//go:noinline
func capTarget191() int { return 191 }

//go:noinline
func capTarget192() int { return 192 }

//go:noinline
func capTarget193() int { return 193 }

//go:noinline
func capTarget194() int { return 194 }

//go:noinline
func capTarget195() int { return 195 }

//go:noinline
func capTarget196() int { return 196 }

//go:noinline
func capTarget197() int { return 197 }

//go:noinline
func capTarget198() int { return 198 }

//go:noinline
func capTarget199() int { return 199 }

//go:noinline
func capTarget200() int { return 200 }

//go:noinline
func capTarget201() int { return 201 }

//go:noinline
func capTarget202() int { return 202 }

//go:noinline
func capTarget203() int { return 203 }

//go:noinline
func capTarget204() int { return 204 }

//go:noinline
func capTarget205() int { return 205 }

//go:noinline
func capTarget206() int { return 206 }

//go:noinline
func capTarget207() int { return 207 }

//go:noinline
func capTarget208() int { return 208 }

//go:noinline
func capTarget209() int { return 209 }

//go:noinline
func capTarget210() int { return 210 }

//go:noinline
func capTarget211() int { return 211 }

//go:noinline
func capTarget212() int { return 212 }

//go:noinline
func capTarget213() int { return 213 }

//go:noinline
func capTarget214() int { return 214 }

//go:noinline
func capTarget215() int { return 215 }

//go:noinline
func capTarget216() int { return 216 }

//go:noinline
func capTarget217() int { return 217 }

//go:noinline
func capTarget218() int { return 218 }

//go:noinline
func capTarget219() int { return 219 }

//go:noinline
func capTarget220() int { return 220 }

//go:noinline
func capTarget221() int { return 221 }

//go:noinline
func capTarget222() int { return 222 }

//go:noinline
func capTarget223() int { return 223 }

//go:noinline
func capTarget224() int { return 224 }

//go:noinline
func capTarget225() int { return 225 }

//go:noinline
func capTarget226() int { return 226 }

//go:noinline
func capTarget227() int { return 227 }

//go:noinline
func capTarget228() int { return 228 }

//go:noinline
func capTarget229() int { return 229 }

//go:noinline
func capTarget230() int { return 230 }

//go:noinline
func capTarget231() int { return 231 }

//go:noinline
func capTarget232() int { return 232 }

//go:noinline
func capTarget233() int { return 233 }

//go:noinline
func capTarget234() int { return 234 }

//go:noinline
func capTarget235() int { return 235 }

//go:noinline
func capTarget236() int { return 236 }

//go:noinline
func capTarget237() int { return 237 }

//go:noinline
func capTarget238() int { return 238 }

//go:noinline
func capTarget239() int { return 239 }

//go:noinline
func capTarget240() int { return 240 }

//go:noinline
func capTarget241() int { return 241 }

//go:noinline
func capTarget242() int { return 242 }

//go:noinline
func capTarget243() int { return 243 }

//go:noinline
func capTarget244() int { return 244 }

//go:noinline
func capTarget245() int { return 245 }

//go:noinline
func capTarget246() int { return 246 }

//go:noinline
func capTarget247() int { return 247 }

//go:noinline
func capTarget248() int { return 248 }

//go:noinline
func capTarget249() int { return 249 }

//go:noinline
func capTarget250() int { return 250 }

//go:noinline
func capTarget251() int { return 251 }

//go:noinline
func capTarget252() int { return 252 }

//go:noinline
func capTarget253() int { return 253 }

//go:noinline
func capTarget254() int { return 254 }

//go:noinline
func capTarget255() int { return 255 }

//go:noinline
func capTarget256() int { return 256 }

//go:noinline
func capTarget257() int { return 257 }

//go:noinline
func capTarget258() int { return 258 }

//go:noinline
func capTarget259() int { return 259 }

//go:noinline
func capTarget260() int { return 260 }

//go:noinline
func capTarget261() int { return 261 }

//go:noinline
func capTarget262() int { return 262 }

//go:noinline
func capTarget263() int { return 263 }

//go:noinline
func capTarget264() int { return 264 }

//go:noinline
func capTarget265() int { return 265 }

//go:noinline
func capTarget266() int { return 266 }

//go:noinline
func capTarget267() int { return 267 }

//go:noinline
func capTarget268() int { return 268 }

//go:noinline
func capTarget269() int { return 269 }

//go:noinline
func capTarget270() int { return 270 }

//go:noinline
func capTarget271() int { return 271 }

//go:noinline
func capTarget272() int { return 272 }

//go:noinline
func capTarget273() int { return 273 }

//go:noinline
func capTarget274() int { return 274 }

//go:noinline
func capTarget275() int { return 275 }

//go:noinline
func capTarget276() int { return 276 }

//go:noinline
func capTarget277() int { return 277 }

//go:noinline
func capTarget278() int { return 278 }

//go:noinline
func capTarget279() int { return 279 }

//go:noinline
func capTarget280() int { return 280 }

//go:noinline
func capTarget281() int { return 281 }

//go:noinline
func capTarget282() int { return 282 }

//go:noinline
func capTarget283() int { return 283 }

//go:noinline
func capTarget284() int { return 284 }

//go:noinline
func capTarget285() int { return 285 }

//go:noinline
func capTarget286() int { return 286 }

//go:noinline
func capTarget287() int { return 287 }

//go:noinline
func capTarget288() int { return 288 }

//go:noinline
func capTarget289() int { return 289 }

//go:noinline
func capTarget290() int { return 290 }

//go:noinline
func capTarget291() int { return 291 }

//go:noinline
func capTarget292() int { return 292 }

//go:noinline
func capTarget293() int { return 293 }

//go:noinline
func capTarget294() int { return 294 }

//go:noinline
func capTarget295() int { return 295 }

//go:noinline
func capTarget296() int { return 296 }

//go:noinline
func capTarget297() int { return 297 }

//go:noinline
func capTarget298() int { return 298 }

//go:noinline
func capTarget299() int { return 299 }

//go:noinline
func capTargets() []interface{} {
	return []interface{}{
		capTarget000, capTarget001, capTarget002, capTarget003, capTarget004, capTarget005, capTarget006, capTarget007,
		capTarget008, capTarget009, capTarget010, capTarget011, capTarget012, capTarget013, capTarget014, capTarget015,
		capTarget016, capTarget017, capTarget018, capTarget019, capTarget020, capTarget021, capTarget022, capTarget023,
		capTarget024, capTarget025, capTarget026, capTarget027, capTarget028, capTarget029, capTarget030, capTarget031,
		capTarget032, capTarget033, capTarget034, capTarget035, capTarget036, capTarget037, capTarget038, capTarget039,
		capTarget040, capTarget041, capTarget042, capTarget043, capTarget044, capTarget045, capTarget046, capTarget047,
		capTarget048, capTarget049, capTarget050, capTarget051, capTarget052, capTarget053, capTarget054, capTarget055,
		capTarget056, capTarget057, capTarget058, capTarget059, capTarget060, capTarget061, capTarget062, capTarget063,
		capTarget064, capTarget065, capTarget066, capTarget067, capTarget068, capTarget069, capTarget070, capTarget071,
		capTarget072, capTarget073, capTarget074, capTarget075, capTarget076, capTarget077, capTarget078, capTarget079,
		capTarget080, capTarget081, capTarget082, capTarget083, capTarget084, capTarget085, capTarget086, capTarget087,
		capTarget088, capTarget089, capTarget090, capTarget091, capTarget092, capTarget093, capTarget094, capTarget095,
		capTarget096, capTarget097, capTarget098, capTarget099, capTarget100, capTarget101, capTarget102, capTarget103,
		capTarget104, capTarget105, capTarget106, capTarget107, capTarget108, capTarget109, capTarget110, capTarget111,
		capTarget112, capTarget113, capTarget114, capTarget115, capTarget116, capTarget117, capTarget118, capTarget119,
		capTarget120, capTarget121, capTarget122, capTarget123, capTarget124, capTarget125, capTarget126, capTarget127,
		capTarget128, capTarget129, capTarget130, capTarget131, capTarget132, capTarget133, capTarget134, capTarget135,
		capTarget136, capTarget137, capTarget138, capTarget139, capTarget140, capTarget141, capTarget142, capTarget143,
		capTarget144, capTarget145, capTarget146, capTarget147, capTarget148, capTarget149, capTarget150, capTarget151,
		capTarget152, capTarget153, capTarget154, capTarget155, capTarget156, capTarget157, capTarget158, capTarget159,
		capTarget160, capTarget161, capTarget162, capTarget163, capTarget164, capTarget165, capTarget166, capTarget167,
		capTarget168, capTarget169, capTarget170, capTarget171, capTarget172, capTarget173, capTarget174, capTarget175,
		capTarget176, capTarget177, capTarget178, capTarget179, capTarget180, capTarget181, capTarget182, capTarget183,
		capTarget184, capTarget185, capTarget186, capTarget187, capTarget188, capTarget189, capTarget190, capTarget191,
		capTarget192, capTarget193, capTarget194, capTarget195, capTarget196, capTarget197, capTarget198, capTarget199,
		capTarget200, capTarget201, capTarget202, capTarget203, capTarget204, capTarget205, capTarget206, capTarget207,
		capTarget208, capTarget209, capTarget210, capTarget211, capTarget212, capTarget213, capTarget214, capTarget215,
		capTarget216, capTarget217, capTarget218, capTarget219, capTarget220, capTarget221, capTarget222, capTarget223,
		capTarget224, capTarget225, capTarget226, capTarget227, capTarget228, capTarget229, capTarget230, capTarget231,
		capTarget232, capTarget233, capTarget234, capTarget235, capTarget236, capTarget237, capTarget238, capTarget239,
		capTarget240, capTarget241, capTarget242, capTarget243, capTarget244, capTarget245, capTarget246, capTarget247,
		capTarget248, capTarget249, capTarget250, capTarget251, capTarget252, capTarget253, capTarget254, capTarget255,
		capTarget256, capTarget257, capTarget258, capTarget259, capTarget260, capTarget261, capTarget262, capTarget263,
		capTarget264, capTarget265, capTarget266, capTarget267, capTarget268, capTarget269, capTarget270, capTarget271,
		capTarget272, capTarget273, capTarget274, capTarget275, capTarget276, capTarget277, capTarget278, capTarget279,
		capTarget280, capTarget281, capTarget282, capTarget283, capTarget284, capTarget285, capTarget286, capTarget287,
		capTarget288, capTarget289, capTarget290, capTarget291, capTarget292, capTarget293, capTarget294, capTarget295,
		capTarget296, capTarget297, capTarget298, capTarget299}
}
