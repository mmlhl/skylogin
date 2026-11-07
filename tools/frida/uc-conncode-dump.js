// 抓 connectCode 的内层明文。
//
// 免密登录时 SDK 会生成一段 connectCode：
//   adat@{"k":RSA(aesKey),"v":1,"d":AES(内层数据)}
// 内层的 AES key 由 SDK 随机生成，只存在于内存里——所以离线解不开，
// 必须在它加密的那一刻把明文截下来。
//
// 只保留「短、且像文本/JSON」的 AES 加密调用，避开大块资源与二进制。

Java.perform(function () {
  var JString = Java.use('java.lang.String');
  var n = 0;
  var m = 0;
  var MAX = 30;

  function emit(tag, s) {
    for (var i = 0; i < s.length; i += 200) {
      console.log(tag + '|' + i + '|' + s.substr(i, 200));
    }
  }

  try {
    var Cipher = Java.use('javax.crypto.Cipher');
    Cipher.doFinal.overload('[B').implementation = function (input) {
      var out = this.doFinal(input);
      try {
        var algo = '' + this.getAlgorithm();
        if (algo.indexOf('AES') < 0 || input === null) return out;
        var len = input.length;

        // ① 免密登录的完整请求：整个 {id, service, data:{connectCode}} 明文
        if (len > 200 && len < 4000 && n < MAX) {
          var s = JString.$new(input, 'UTF-8').toString();
          if (s.indexOf('loginBySilentCodeService') >= 0) {
            n++;
            console.log('[SILENT' + n + '] len=' + len);
            emit('S' + n, s);
            return out;
          }
        }

        // ② 小块 AES 加密：connectCode 内层很可能落在这一段
        if (len >= 16 && len <= 400 && m < MAX) {
          var t = JString.$new(input, 'UTF-8').toString();
          if (t.indexOf('{') >= 0 || t.indexOf(':') >= 0 || t.indexOf('=') >= 0) {
            m++;
            console.log('[SMALL' + m + '] len=' + len);
            emit('M' + m, t);
          }
        }
      } catch (e) {}
      return out;
    };
    console.log('[conncode-dump] 已安装');
  } catch (e) {
    console.log('[conncode-dump] 安装失败: ' + e);
  }
});
