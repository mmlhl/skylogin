// 打印 SDK 实际用于 RSA 的公钥模数，与内置公钥对比。
//
// Frida 里不能直接对任意对象调方法，要先用 Java.cast 转成接口类型。
// 这里同时打印 key 的真实类名，便于判断有没有走别的实现。

Java.perform(function () {
  var RSAPub = null;
  try {
    RSAPub = Java.use('java.security.interfaces.RSAPublicKey');
  } catch (e) {
    console.log('[PUB] 取 RSAPublicKey 接口失败: ' + e);
  }

  function show(key) {
    try {
      var cls = '' + key.$className;
      if (cls.indexOf('RSA') < 0) return;
      // Conscrypt 的实现不允许反射调 getModulus，但 toString() 会把
      // modulus 与 publicExponent 以十六进制打印出来，足够对比用。
      var s = '' + key.toString();
      console.log('[PUB] class=' + cls);
      console.log('[PUB] ' + s);
    } catch (e) {
      console.log('[PUB] 读取失败: ' + e);
    }
  }

  try {
    var Cipher = Java.use('javax.crypto.Cipher');
    var init = Cipher.init.overload('int', 'java.security.Key');
    init.implementation = function (mode, key) {
      try { show(key); } catch (e) {}
      return this.init(mode, key);
    };
    console.log('[uc-pub-dump2] 已安装');
  } catch (e) {
    console.log('[uc-pub-dump2] 安装失败: ' + e);
  }
});
