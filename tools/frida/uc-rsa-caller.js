// 目标：建立"调用方 → 公钥 → 请求"的对应关系。
//
// 已确认 SDK 会用至少三把不同的 1024 位公钥：
//   AKjnX9Dk…  最先出现，早于 getSecurityKey 下发，疑为内置引导公钥
//   AMFxR3Nx…  与 getSecurityKey 的 K2 一致，也就是我们一直在用的那把
//   IMYlSGNB…  第三把，用途未知
//
// 只用一把去加密所有请求必然失败，所以必须先把对应关系钉死。
// 这里在 Cipher.init 时打印调用栈，直接看出是哪个业务类在用哪把 key。

Java.perform(function () {
  var Base64 = Java.use('android.util.Base64');
  var Throwable = Java.use('java.lang.Throwable');

  function b64(b) {
    if (b === null || b === undefined) return 'null';
    try { return Base64.encodeToString(b, 2); } catch (e) { return 'ERR'; }
  }

  function hex(b, limit) {
    if (b === null || b === undefined) return 'null';
    var n = limit ? Math.min(b.length, limit) : b.length;
    var s = '';
    for (var i = 0; i < n; i++) {
      var v = b[i];
      if (v < 0) v += 256;
      s += ('0' + v.toString(16)).slice(-2);
    }
    return s;
  }

  // 只保留业务侧的帧，跳过 JDK / Android 框架自身的调用
  function stack() {
    try {
      var frames = Throwable.$new().getStackTrace();
      var out = [];
      for (var i = 0; i < frames.length && out.length < 8; i++) {
        var f = frames[i].toString();
        if (/^(java|javax|kotlin|dalvik|android|com\.android)\./.test(f)) continue;
        out.push(f);
      }
      return out.join(' | ');
    } catch (e) {
      return 'ERR(' + e + ')';
    }
  }

  var Cipher = Java.use('javax.crypto.Cipher');
  // 同一个 Cipher 实例先 init 再 doFinal，这里按实例记下它用的公钥
  var derByInstance = {};

  function record(cipher, key) {
    var der = 'null';
    try { der = b64(key.getEncoded()); } catch (e) { der = 'ERR'; }
    try { derByInstance[cipher.hashCode()] = der; } catch (e) {}
    console.log('[KEY] ' + der);
    console.log('[WHO] ' + stack());
    return der;
  }

  function isRSA(cipher) {
    try {
      return (cipher.getAlgorithm() || '').toUpperCase().indexOf('RSA') >= 0;
    } catch (e) {
      return false;
    }
  }

  try {
    Cipher.init.overload('int', 'java.security.Key').implementation = function () {
      var r = this.init.apply(this, arguments);
      try { if (isRSA(this)) record(this, arguments[1]); } catch (e) {}
      return r;
    };
  } catch (e) { console.log('init 失败 ' + e); }

  try {
    Cipher.init.overload('int', 'java.security.Key', 'java.security.SecureRandom').implementation = function () {
      var r = this.init.apply(this, arguments);
      try { if (isRSA(this)) record(this, arguments[1]); } catch (e) {}
      return r;
    };
  } catch (e) { console.log('initR 失败 ' + e); }

  try {
    Cipher.doFinal.overload('[B').implementation = function (input) {
      var out = this.doFinal(input);
      try {
        if (isRSA(this)) {
          var h = this.hashCode();
          console.log('[RSA] in=' + (input ? input.length : 0) + ' out=' + (out ? out.length : 0));
          console.log('[RSA] DER=' + (derByInstance[h] || '?'));
          console.log('[RSA] IN =' + hex(input));
          console.log('[RSA] OUT=' + b64(out));
        }
      } catch (e) {}
      return out;
    };
  } catch (e) { console.log('doFinal 失败 ' + e); }

  console.log('[uc-rsa-caller] 已安装');
});
