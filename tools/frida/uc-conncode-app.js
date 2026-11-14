// 抓九游 App 生成 connectCode 时被加密的明文（v5，稳定优先）。
//
// 为什么这版不一样：
//   v2 / v3 / v4 都 Hook 了 StringBuilder.toString，而 StringBuilder 是 ART
//   里被内联优化的高频类，Hook 它的 toString 会破坏编译器假设，直接崩进程
//   （实测连续两次闪退）。对比稳定运行的 uc-crypt-dump，它只挂了下面两个：
//       javax.crypto.Cipher.doFinal('[B')
//       javax.crypto.spec.SecretKeySpec.$init('[B','java.lang.String')
//   v5 的 Hook 面就严格等于这两个，一个都不多。
//
// 拿不到 "adat@" 成品没关系：connectCode 本身就是
//       "adat@" + {"k": RSA(AES key), "v": 1, "d": AES(明文)}
//   只要抓到「RSA 加密 16 字节输入」（= k）和「AES 加密的可读 JSON」（= d），
//   拼装逻辑就不重要了。
//
// 定位生成逻辑改用**类枚举**：只读已加载类的名字和方法名，不拦截任何调用，
// 因此不会影响程序行为。枚举在启动后延迟执行，等 SDK 类加载完。
//
// 日志标签：
//   [ENCn] AES 加密的明文（原样输出，不 base64）
//   [RSAn] RSA 加密的输入（16 字节 = 被包裹的 AES key/iv）
//   [KEYn] SecretKeySpec
//   [CLS*] 类枚举结果（找 connectCode 生成器）
Java.perform(function () {
  var JString = Java.use('java.lang.String');
  var Base64 = Java.use('android.util.Base64');

  var MAX = 60;
  var encN = 0, rsaN = 0, keyN = 0;
  var started = Date.now();
  var rescan = 0; // 类表补扫次数上限

  // 遥测固定字段，命中即丢，否则日志被刷爆
  var NOISE = ['ac_action', 'abtests', 'wp_monitor', 'ac_session_id', 'unique_log_id'];

  function isNoise(s) {
    for (var i = 0; i < NOISE.length; i++) {
      if (s.indexOf(NOISE[i]) >= 0) return true;
    }
    return false;
  }

  function printable(bytes) {
    if (bytes === null || bytes.length === 0) return null;
    var s;
    try { s = JString.$new(bytes, 'UTF-8').toString(); } catch (e) { return null; }
    if (s.length === 0) return null; // 随机二进制会解出空串，挡掉
    var ok = 0;
    for (var i = 0; i < s.length; i++) {
      var c = s.charCodeAt(i);
      if ((c >= 0x20 && c < 0x7f) || c === 0x0a || c === 0x0d || c === 0x09) ok++;
    }
    return (ok === s.length) ? s : null;
  }

  function b64(bytes) {
    try { return Base64.encodeToString(bytes, 2); } catch (e) { return 'ERR'; }
  }

  function emit(tag, s) {
    for (var i = 0; i < s.length; i += 200) console.log(tag + '|' + i + '|' + s.substr(i, 200));
  }

  // ==== 唯一的两个 Hook：与 uc-crypt-dump 完全一致 ====
  try {
    var Cipher = Java.use('javax.crypto.Cipher');
    Cipher.doFinal.overload('[B').implementation = function (input) {
      var out = this.doFinal(input);
      try {
        var inLen = input === null ? 0 : input.length;
        if (inLen > 0) {
          var algo = '' + this.getAlgorithm();
          var mode = -1;
          try { mode = this.getOpMode(); } catch (e) {}

          // 只看加密方向；解密方向没有我们要的东西
          if (mode === 1) {
            // 疑似 connectCode 的加密事件出现时补扫类（那会儿 SDK 一定已加载）。
            // 只补扫有限次，避免每次遥测加密都去遍历类表。
            if (rescan < 3 && ((algo.indexOf('RSA') >= 0 && inLen === 16) ||
                (algo.indexOf('AES') >= 0 && inLen > 64))) {
              rescan++;
              try { scanClasses(); } catch (e) {}
            }
            if (algo.indexOf('RSA') >= 0 && inLen <= 64 && rsaN < MAX) {
              rsaN++;
              var t = printable(input);
              console.log('[RSA' + rsaN + '] len=' + inLen + ' t=' + Date.now());
              console.log('R' + rsaN + '|b64|' + b64(input));
              if (t !== null) console.log('R' + rsaN + '|txt|' + t);
            } else if (algo.indexOf('AES') >= 0 && encN < MAX) {
              var s = printable(input);
              if (s !== null && s.length <= 3000 && !isNoise(s) && /[\{\}"=:]/.test(s)) {
                encN++;
                console.log('[ENC' + encN + '] algo=' + algo + ' len=' + inLen + ' t=' + Date.now());
                emit('E' + encN, s);
              }
            }
          }
        }
      } catch (e) {}
      return out;
    };
    console.log('[conncode-v5] Cipher 已挂');
  } catch (e) { console.log('[conncode-v5] Cipher 失败: ' + e); }

  try {
    var KeySpec = Java.use('javax.crypto.spec.SecretKeySpec');
    KeySpec.$init.overload('[B', 'java.lang.String').implementation = function (k, alg) {
      try {
        if (k !== null && k.length >= 16 && keyN < MAX) {
          var txt = printable(k);
          if (txt !== 'smkldospdosldaaa') { // 遥测固定 key
            keyN++;
            console.log('[KEY' + keyN + '] ' + alg + ' len=' + k.length + ' t=' + Date.now());
            console.log('K' + keyN + '|' + (txt !== null ? 'txt|' + txt : 'b64|' + b64(k)));
          }
        }
      } catch (e) {}
      return this.$init(k, alg);
    };
    console.log('[conncode-v5] SecretKeySpec 已挂');
  } catch (e) { console.log('[conncode-v5] SecretKeySpec 失败: ' + e); }

  // ==== 类枚举：只读名字，不拦截调用，安全 ====
  var clsN = 0;
  // 返回 true 表示这个类命中了感兴趣的方法
  function dumpMethods(name) {
    if (clsN >= 40) return false;
    try {
      var C = Java.use(name);
      var ms = C.class.getDeclaredMethods();
      var hits = [];
      for (var i = 0; i < ms.length && hits.length < 25; i++) {
        var m = '' + ms[i].getName();
        if (/connect|silent|ngcode|ticket|loginby|applycode/i.test(m)) hits.push(m);
      }
      if (hits.length) {
        clsN++;
        console.log('[CLS' + clsN + '] ' + name + ' :: ' + hits.join(','));
        return true;
      }
    } catch (e) {}
    return false;
  }

  // 注意：这套运行时里异步回调（setTimeout / enumerateLoadedClasses 的
  // onMatch/onComplete）都不送回来，实测一条都没打印。只能用同步版 API。
  function scanClasses() {
    try {
      var all = Java.enumerateLoadedClassesSync();
      // 框架/三方库一律不看，否则 30 个名额会被 android.media.Metadata 这类占满
      var SKIP = /^(android\.|com\.android\.|java\.|javax\.|kotlin\.|dalvik\.|org\.|com\.google\.|com\.alibaba\.|okhttp|retrofit|okio)/;
      var cand = [];
      for (var i = 0; i < all.length; i++) {
        var n = all[i];
        if (SKIP.test(n)) {
          // 例外：阿里 SDK 自己可能包着 connectCode，单独留一个口子
          if (/connectcode|silentcode/i.test(n)) cand.push(n);
          continue;
        }
        // 自家包（九游/SDK/网易）全收；其它包只收名字里带 connect/silent 的
        if (/(ninegame|gamemanager|aligames|ucsdk|ngsdk|netease)/i.test(n)) { cand.push(n); continue; }
        if (/connect ?code|silent ?code|ngcode|silentlogin|connectlogin/i.test(n)) cand.push(n);
      }
      console.log('[conncode-v5] 已加载类 ' + all.length + ' 个，候选 ' + cand.length + ' 个');
      var hit = 0;
      for (var j = 0; j < cand.length && clsN < 40; j++) {
        if (dumpMethods(cand[j])) hit++;
      }
      console.log('[conncode-v5] 枚举完成，命中 ' + hit + ' 个');
    } catch (e) { console.log('[conncode-v5] 类枚举失败: ' + e); }
  }

  // 注意：这套 Frida 运行时里的 setTimeout 是空壳，回调永远不会触发
  //（实测日志里一条枚举结果都没有），所以改成同步跑一次。
  // 加载时机早，SDK 类可能还没进内存，所以另外在加密钩子里按需补扫。
  var scanDone = false;
  try { scanClasses(); scanDone = true; } catch (e) { console.log('[conncode-v5] 首次枚举失败: ' + e); }

  console.log('[conncode-v5] 就绪（钩子面 = Cipher + SecretKeySpec）');
});
