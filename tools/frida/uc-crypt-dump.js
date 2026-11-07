// 九游 / UC SDK 加密流量观测脚本
//
// 用途：把 UC SDK 每次请求的「明文 / 密文 / 密钥」打印出来，
// 用于对齐加密信封（AES 的 key/iv 怎么用、RSA 公钥怎么下发）。
//
// 用法：通过设备端 hook 工具导入本文件，绑定到目标进程后重启该进程。
// 输出较多，建议只在需要分析时启用。

Java.perform(function () {
  var Base64 = Java.use('android.util.Base64');
  var MAX = 120;
  var count = 0;

  // 分片输出：单条日志有长度上限，长内容必须切开，否则尾部会丢
  function emit(tag, s) {
    for (var i = 0; i < s.length; i += 240) {
      console.log(tag + '|' + i + '|' + s.substr(i, 240));
    }
  }

  function encode(bytes) {
    try {
      if (bytes === null) return 'null';
      return Base64.encodeToString(bytes, 2);
    } catch (e) {
      return 'ERR:' + e;
    }
  }

  try {
    var Cipher = Java.use('javax.crypto.Cipher');
    Cipher.doFinal.overload('[B').implementation = function (input) {
      var out = this.doFinal(input);
      try {
        var inLen = input === null ? 0 : input.length;
        var outLen = out === null ? 0 : out.length;
        // 太短的调用多为心跳/校验，噪声大，跳过
        if (inLen >= 32 && count < MAX) {
          count++;
          console.log('[C' + count + '] ' + this.getAlgorithm() + ' in=' + inLen + ' out=' + outLen);
          emit('I' + count, encode(input));
          emit('O' + count, encode(out));
        }
      } catch (e) {}
      return out;
    };
  } catch (e) {
    console.log('hook Cipher 失败: ' + e);
  }

  // 对称密钥的构造点，用来确认 key/iv 是随机生成还是固定值
  try {
    var KeySpec = Java.use('javax.crypto.spec.SecretKeySpec');
    KeySpec.$init.overload('[B', 'java.lang.String').implementation = function (k, alg) {
      try { console.log('[KEY] ' + alg + ' ' + encode(k)); } catch (e) {}
      return this.$init(k, alg);
    };
  } catch (e) {}

  try {
    var IvSpec = Java.use('javax.crypto.spec.IvParameterSpec');
    IvSpec.$init.overload('[B').implementation = function (iv) {
      try { console.log('[IV] ' + encode(iv)); } catch (e) {}
      return this.$init(iv);
    };
  } catch (e) {}

  console.log('[uc-crypt-dump] 已安装');
});
