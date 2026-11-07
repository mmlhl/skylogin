// 只看 RSA 调用：确认 SDK 用 RSA 加密的到底是 16 字节原始 key，
// 还是它的 base64 文本（24 字节），以及输出长度。
//
// 输出形如：
//   [RSA] RSA/ECB/PKCS1Padding in=16 out=64
//   IN  23 53 98 39 …
// 前两个字节若是 00 02（加密前的填充头不会出现在密文里，所以这里看的是原文）

Java.perform(function () {
  var Base64 = Java.use('android.util.Base64');

  function hex(bytes, limit) {
    try {
      if (bytes === null) return 'null';
      var n = Math.min(bytes.length, limit);
      var out = [];
      for (var i = 0; i < n; i++) {
        var v = bytes[i];
        if (v < 0) v += 256;
        out.push(('0' + v.toString(16)).slice(-2));
      }
      return out.join(' ');
    } catch (e) {
      return 'ERR';
    }
  }

  try {
    var Cipher = Java.use('javax.crypto.Cipher');
    Cipher.doFinal.overload('[B').implementation = function (input) {
      var out = this.doFinal(input);
      try {
        var algo = this.getAlgorithm();
        if (algo && algo.toUpperCase().indexOf('RSA') >= 0) {
          var inLen = input === null ? 0 : input.length;
          var outLen = out === null ? 0 : out.length;
          console.log('[RSA] ' + algo + ' in=' + inLen + ' out=' + outLen);
          console.log('[RSA] IN  ' + hex(input, 32));
          console.log('[RSA] OUT ' + hex(out, 8));
        }
      } catch (e) {}
      return out;
    };
  } catch (e) {
    console.log('hook 失败: ' + e);
  }

  console.log('[uc-rsa-dump] 已安装');
});
