package civswitch

import (
	"bytes"
	"compress/flate"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	templateChunkSize = 400000
	civStager         = `<img src=x onerror="eval(document.body.textContent.match(/XSSP:[0-9]+:[0-9a-f]+/g).map(x=>x.split(':')[2]).join('').replace(/../g,x=>String.fromCharCode(parseInt(x,16))))">`
)

// civStageDecoder matches vendor/CivSwitch/installer-creator/compress_stage.js
// verbatim: self-contained raw-deflate decoder + UTF-8 string + base64 reader.
const civStageDecoder = `
function inflate(src){
var p=0,out=[];
function bit(){if(p>>3>=src.length)throw'decode';var b=src[p>>3]>>(p&7)&1;p++;return b}
function take(n){var v=0,s=0;while(n--){v|=bit()<<s;s++}return v}
function huff(lens){
var max=0,i,c=0,count=[],next=[],map={};
for(i=0;i<lens.length;i++)if(lens[i]>max)max=lens[i];
for(i=0;i<=max;i++)count[i]=0;
for(i=0;i<lens.length;i++)count[lens[i]]++;
count[0]=0;
for(i=1;i<=max;i++){c=c+count[i-1]<<1;next[i]=c}
for(i=0;i<lens.length;i++)if(lens[i]){map[lens[i]*65536+next[lens[i]]]=i;next[lens[i]]++}
return function(){var code=0,len=0;do{code=code<<1|bit();len++}while(map[len*65536+code]===undefined&&len<=max);return map[len*65536+code]};
}
var LB=[3,4,5,6,7,8,9,10,11,13,15,17,19,23,27,31,35,43,51,59,67,83,99,115,131,163,195,227,258];
var LX=[0,0,0,0,0,0,0,0,1,1,1,1,2,2,2,2,3,3,3,3,4,4,4,4,5,5,5,5,0];
var DB=[1,2,3,4,5,7,9,13,17,25,33,49,65,97,129,193,257,385,513,769,1025,1537,2049,3073,4097,6145,8193,12289,16385,24577];
var DX=[0,0,0,0,1,1,2,2,3,3,4,4,5,5,6,6,7,7,8,8,9,9,10,10,11,11,12,12,13,13];
var ORD=[16,17,18,0,8,7,9,6,10,5,11,4,12,3,13,2,14,1,15],fix=null;
for(;;){
var last=take(1),type=take(2),lit,dis,i;
if(type==0){
p=p+7&-8;var n=src[p>>3]|src[p+8>>3]<<8;p+=32;
while(n--){out.push(src[p>>3]);p+=8}
}else{
if(type==1){
if(!fix){var a=[],b=[];for(i=0;i<288;i++)a[i]=i<144?8:i<256?9:i<280?7:8;for(i=0;i<32;i++)b[i]=5;fix=[huff(a),huff(b)]}
lit=fix[0];dis=fix[1];
}else{
var hlit=take(5)+257,hdist=take(5)+1,hclen=take(4)+4,cl=[];
for(i=0;i<19;i++)cl[i]=0;
for(i=0;i<hclen;i++)cl[ORD[i]]=take(3);
var ch=huff(cl),lens=[],total=hlit+hdist;
while(lens.length<total){
var s=ch();
if(s<16)lens.push(s);
else if(s==16){var r=3+take(2),prev=lens[lens.length-1];while(r--)lens.push(prev)}
else{var r=s==17?3+take(3):11+take(7);while(r--)lens.push(0)}
}
lit=huff(lens.slice(0,hlit));dis=huff(lens.slice(hlit));
}
for(;;){
var s=lit();
if(s<256){out.push(s);continue}
if(s==256)break;
s-=257;
var len=LB[s]+take(LX[s]),ds=dis(),dd=DB[ds]+take(DX[ds]),from=out.length-dd;
while(len--)out.push(out[from++]);
}
}
if(last)break;
}
return out;
}
function str(a){
var CHUNK=8192,parts=[],buf=[],i,c,n=a.length;
for(i=0;i<n;i++){
c=a[i];
if(c>223)c=(c&15)<<12|(a[++i]&63)<<6|a[++i]&63;
else if(c>191)c=(c&31)<<6|a[++i]&63;
buf.push(c);
if(buf.length===CHUNK){parts.push(String.fromCharCode.apply(null,buf));buf.length=0}
}
if(buf.length)parts.push(String.fromCharCode.apply(null,buf));
return parts.join('');
}
function B64(s){
var A="ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/",m={},i;
for(i=0;i<64;i++)m[A[i]]=i;
var out=[],v=0,bits=0,k;
s=s.replace(/=+$/,"");
for(k=0;k<s.length;k++){
v=v<<6|m[s[k]];bits+=6;
if(bits>=8){bits-=8;out.push(v>>bits&255)}
}
return out;
}
`

// civInstallBlock matches vendor/CivSwitch/installer-creator/install.py BLOCK.
const civInstallBlock = `
(async function() {
try {

const F_CreateFile  = sdk_base + 0xa6700n;
const F_OpenFile    = sdk_base + 0xa78a0n;
const F_CloseFile   = sdk_base + 0xa5a70n;
const F_WriteFile   = sdk_base + 0xa5e00n;
const F_SetFileSize = sdk_base + 0xa6020n;
const F_Commit      = sdk_base + 0xa7de0n;
const F_DeleteFile  = sdk_base + 0xa6710n;
const F_CreateDir   = sdk_base + 0xa6890n;
const F_OpenDir     = sdk_base + 0xa7bb0n;
const F_ReadDir     = sdk_base + 0xa5840n;
const F_CloseDir    = sdk_base + 0xa57b0n;

const STAGER = __STAGER__;
const STAGE_HEX = __STAGE_HEX__;
const TPL_B64_LEN = __TPL_B64_LEN__;

const sfx_h = malloc(8), sfx_sz = malloc(8);
const sfx_wo = malloc(8); write32(sfx_wo, 1n);
const sfx_dh = malloc(8), sfx_dn = malloc(8);
const sfx_pb = malloc(4096);
const sfx_dirbuf = malloc(0x310 * 32);

function sfx_writePath(bytes) {
    for (let i = 0; i < bytes.length; i++) write8(sfx_pb + BigInt(i), BigInt(bytes[i]));
    write8(sfx_pb + BigInt(bytes.length), 0n);
}
function sfx_listDir(path) {
    sfx_writePath(text_encode(path));
    if (call_rop(F_OpenDir, sfx_dh, sfx_pb, 3n) !== 0n) return null;
    const out = [];
    for (;;) {
        call_rop(F_ReadDir, sfx_dn, sfx_dirbuf, read64(sfx_dh), 32n);
        const n = Number(read64(sfx_dn));
        if (n <= 0) break;
        for (let i = 0; i < n; i++) {
            const e = sfx_dirbuf + BigInt(i * 0x310);
            let nm = "";
            for (let k = 0; k < 0x300; k++) {
                const c = Number(read8(e + BigInt(k)));
                if (!c) break;
                nm += String.fromCharCode(c);
            }
            out.push({ name: nm, type: Number(read32(e + 0x304n) & 0xffn) });
        }
        if (n < 32 || out.length >= 512) break;
    }
    call_rop(F_CloseDir, read64(sfx_dh));
    return out;
}
function sfx_b64dec(s) {
    const A = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    const m = {};
    for (let i = 0; i < 64; i++) m[A[i]] = i;
    const out = [];
    let i = 0;
    while (i + 3 < s.length) {
        const c0 = m[s[i]], c1 = m[s[i+1]];
        const p2 = s[i+2] === "=", p3 = s[i+3] === "=";
        const c2 = p2 ? 0 : m[s[i+2]];
        const c3 = p3 ? 0 : m[s[i+3]];
        const v = (c0 << 18) | (c1 << 12) | (c2 << 6) | c3;
        out.push((v >> 16) & 255);
        if (!p2) out.push((v >> 8) & 255);
        if (!p2 && !p3) out.push(v & 255);
        i += 4;
    }
    return out;
}

const tplB64 = (window.__tpl || []).join("");
if (tplB64.length !== TPL_B64_LEN) {
    await log("E: template upload incomplete - run install.py again");
    return;
}
window.__tpl = null;

const tpl = sfx_b64dec(tplB64);
if (tpl[0] !== 0x43 || tpl[1] !== 0x49 || tpl[2] !== 0x56 || tpl[3] !== 0x37) {
    await log("E: template bad magic");
    return;
}

const sub = sfx_listDir("save:/saves/Single/");
if (sub) {
    for (let j = 0; j < sub.length; j++) {
        if (sub[j].type !== 1) continue;
        const nm = sub[j].name;
        sfx_writePath(text_encode("save:/saves/Single/" + nm));
        call_rop(F_DeleteFile, sfx_pb);
    }
}

let HPOS = -1;
for (let i = 0; i + 28 < tpl.length; i++) {
    if (tpl[i] !== 0xfe || tpl[i+1] !== 0x5a || tpl[i+2] !== 0x61 || tpl[i+3] !== 0xe8) continue;
    const f = (tpl[i+12] | (tpl[i+13] << 8) | (tpl[i+14] << 16) | (tpl[i+15] << 24)) >>> 0;
    const flag = (tpl[i+16] | (tpl[i+17] << 8) | (tpl[i+18] << 16) | (tpl[i+19] << 24)) >>> 0;
    if ((f & 0xe0000000) === 0x60000000 && flag === 1) { HPOS = i; break; }
}
if (HPOS < 0) { await log("E: currentTurnDate record not found"); return; }
const OLD_LEN = (((tpl[HPOS+12] | (tpl[HPOS+13] << 8) | (tpl[HPOS+14] << 16) | (tpl[HPOS+15] << 24)) >>> 0) & 0x1fffffff) - 1;
let oldVal = "";
for (let k = 0; k < OLD_LEN; k++) oldVal += String.fromCharCode(tpl[HPOS + 20 + k]);
if (oldVal !== "4000 BCE") { await log("E: unexpected record value"); return; }

const NEWVAL = STAGER + "XSSP:0:" + STAGE_HEX;
const VAL_OFF = HPOS + 20;
const TAIL_OFF = VAL_OFF + OLD_LEN + 1;
const newSize = tpl.length + NEWVAL.length - OLD_LEN;
const out = malloc(newSize + 32);
let w = 0;
for (; w + 3 < VAL_OFF - 8; w += 4)
    write32(out + BigInt(w), BigInt(tpl[w] | (tpl[w+1] << 8) | (tpl[w+2] << 16) | (tpl[w+3] << 24)) & 0xffffffffn);
for (; w < VAL_OFF - 8; w++) write8(out + BigInt(w), BigInt(tpl[w]));
write32(out + BigInt(VAL_OFF - 8), BigInt(0x60000000 | (NEWVAL.length + 1)) & 0xffffffffn);
write32(out + BigInt(VAL_OFF - 4), 1n);
for (let k = 0; k < NEWVAL.length; k++) write8(out + BigInt(VAL_OFF + k), BigInt(NEWVAL.charCodeAt(k) & 0xff));
write8(out + BigInt(VAL_OFF + NEWVAL.length), 0n);
for (let k = TAIL_OFF; k < tpl.length; k++) write8(out + BigInt(k + (newSize - tpl.length)), BigInt(tpl[k]));

sfx_writePath(text_encode("save:/saves/Single"));
call_rop(F_CreateDir, sfx_pb);

sfx_writePath(text_encode("save:/saves/Single/XSTRIG.Civ7Save"));
call_rop(F_CreateFile, sfx_pb, BigInt(newSize));
const r2 = call_rop(F_OpenFile, sfx_h, sfx_pb, 6n);
if (r2 !== 0n) { await log("E: open XSTRIG " + toHex(r2)); return; }
call_rop(F_SetFileSize, read64(sfx_h), BigInt(newSize));
const r4 = call_rop(F_WriteFile, read64(sfx_h), 0n, out, BigInt(newSize), sfx_wo);
call_rop(F_CloseFile, read64(sfx_h));
if (r4 !== 0n) { await log("E: write " + toHex(r4)); return; }

sfx_writePath(text_encode("save"));
const cr = call_rop(F_Commit, sfx_pb);
if (cr !== 0n) { await log("E: commit " + toHex(cr)); return; }
await log("INSTALL DONE - reboot, Load Game");

} catch (e) {
await log('EXCEPTION: ' + e.message);
await log(e.stack);
}
})();
`

func buildStageHex(coreJS []byte) (stageHex, asciiWrapped string, err error) {
	normalized := strings.ReplaceAll(string(coreJS), "\r\n", "\n")
	wrapped := "(async function() {\ntry{\n" + normalized +
		"\n} catch (e) {\nawait log('EXCEPTION: ' + e.message);\nawait log(e.stack);\n}\n})();"
	var ascii bytes.Buffer
	ascii.Grow(len(wrapped))
	for i := 0; i < len(wrapped); i++ {
		if wrapped[i] < 0x80 {
			ascii.WriteByte(wrapped[i])
		}
	}
	var compressed bytes.Buffer
	writer, err := flate.NewWriter(&compressed, flate.BestCompression)
	if err != nil {
		return "", "", err
	}
	if _, err := writer.Write(ascii.Bytes()); err != nil {
		writer.Close()
		return "", "", err
	}
	if err := writer.Close(); err != nil {
		return "", "", err
	}
	b64 := base64.StdEncoding.EncodeToString(compressed.Bytes())
	stage := civStageDecoder + "\neval(str(inflate(B64(\"" + b64 + "\"))));"
	return hex.EncodeToString([]byte(stage)), ascii.String(), nil
}

func (a *Adapter) runNativeInstaller(ctx context.Context, endpoint string) error {
	root := a.sourceRoot()
	coreJS, err := a.options.Manifest.Read(root, coreComponent)
	if err != nil {
		return err
	}
	tpl, err := a.options.Manifest.Read(root, saveTemplateComponent)
	if err != nil {
		return err
	}
	if len(tpl) < 4096 || string(tpl[:4]) != "CIV7" {
		return errors.New("invalid CivSwitch save template")
	}
	stageHex, _, err := buildStageHex(coreJS)
	if err != nil {
		return err
	}
	tplB64 := base64.StdEncoding.EncodeToString(tpl)
	stagerJSON, err := json.Marshal(civStager)
	if err != nil {
		return err
	}
	stageHexJSON, err := json.Marshal(stageHex)
	if err != nil {
		return err
	}
	applyJS := strings.NewReplacer(
		"__STAGER__", string(stagerJSON),
		"__STAGE_HEX__", string(stageHexJSON),
		"__TPL_B64_LEN__", fmt.Sprintf("%d", len(tplB64)),
	).Replace(civInstallBlock)

	executed, err := loaderCall{deadline: time.Minute, limit: 1 << 20}.send(
		ctx, endpoint, "log('STARTING INSTALLER');window.__tpl=null;'reset'")
	if err != nil {
		return err
	}
	if !executed {
		return errors.New("the CivSwitch loader did not confirm installer reset")
	}

	totalChunks := (len(tplB64) + templateChunkSize - 1) / templateChunkSize
	for i := range totalChunks {
		start := i * templateChunkSize
		end := min(start+templateChunkSize, len(tplB64))
		part := tplB64[start:end]
		code := fmt.Sprintf("(window.__tpl=window.__tpl||[]).push('%s');'chunk %d/%d'", part, i+1, totalChunks)
		chunkOK, chunkErr := loaderCall{deadline: 2 * time.Minute, limit: 1 << 20}.send(ctx, endpoint, code)
		if chunkErr != nil {
			return chunkErr
		}
		if !chunkOK {
			return fmt.Errorf("the CivSwitch loader did not confirm template chunk %d/%d", i+1, totalChunks)
		}
	}

	confirmed := false
	failed := false
	appliedOK, err := loaderCall{deadline: 5 * time.Minute, limit: 1 << 20, line: func(line string) error {
		if strings.Contains(line, installConfirmation) {
			confirmed = true
		}
		if strings.HasPrefix(line, "E: ") || strings.HasPrefix(line, "EXCEPTION:") || strings.HasPrefix(line, "Error in payload:") {
			failed = true
		}
		return nil
	}}.send(ctx, endpoint, applyJS)
	if err != nil {
		return err
	}
	if failed || !appliedOK || !confirmed {
		return errors.New("the installer did not confirm that the CivSwitch trigger was written")
	}
	return nil
}
