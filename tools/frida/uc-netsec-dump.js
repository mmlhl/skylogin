// 目标：把"请求内容 → 使用的公钥"直接对上。
//
// 上一步已确认：异步 post 走内置公钥 AKjn…，同步 post 走下发公钥 AMFx…。
// 但这只是实现分支，不代表服务端的分工。真正要回答的是：
// getSecurityKey 这条请求本身，加密用的是哪一把。
//
// 这里 hook 协议层入口 NetworkSecurity.encrypt，打印请求体与本次所用公钥。

Java.perform(function () {
  var Base64 = Java.use('android.util.Base64');
  var lastKey = null;

  function b64(b) {
    if (b === null || b === undefined) return 'null';
    try { return Base64.encodeToString(b, 2); } catch (e) { return 'ERR'; }
  }

  function isRSA(c) {
    try { return (c.getAlgorithm() || '').toUpperCase().indexOf('RSA') >= 0; } catch (e) { return false; }
  }

  var Cipher = Java.use('javax.crypto.Cipher');

  try {
    Cipher.init.overload('int', 'java.security.Key').implementation = function () {
      var r = this.init.apply(this, arguments);
      try { if (isRSA(this)) lastKey = b64(arguments[1].getEncoded()); } catch (e) {}
      return r;
    };
  } catch (e) { console.log('init 失败 ' + e); }

  try {
    Cipher.init.overload('int', 'java.security.Key', 'java.security.SecureRandom').implementation = function () {
      var r = this.init.apply(this, arguments);
      try { if (isRSA(this)) lastKey = b64(arguments[1].getEncoded()); } catch (e) {}
      return r;
    };
  } catch (e) { console.log('initR 失败 ' + e); }

  try {
    var NS = Java.use('cn.uc.gamesdk.lib.network.protocol.NetworkSecurity');
    console.log('[NS] 类已找到');

    // 先摸清这个类有哪些方法，签名不同则 hook 方式也不同
    try {
      var ms = NS.class.getDeclaredMethods();
      for (var i = 0; i < ms.length; i++) {
        var ps = ms[i].getParameterTypes();
        var names = [];
        for (var j = 0; j < ps.length; j++) names.push(ps[j].getName());
        console.log('[NS-M] ' + ms[i].getName() + '(' + names.join(',') + ')');
      }
    } catch (e) {
      console.log('[NS-M] 枚举失败 ' + e);
    }

    NS.encrypt.overloads.forEach(function (ov) {
      ov.implementation = function () {
        lastKey = null;
        var r = this.encrypt.apply(this, arguments);
        var parts = [];
        for (var k = 0; k < arguments.length; k++) {
          var v = arguments[k];
          try {
            parts.push(v === null ? 'null' : String(v));
          } catch (e) {
            parts.push('?');
          }
        }
        console.log('[ENC] key=' + lastKey);
        console.log('[ENC] args=' + parts.join(' || '));
        console.log('[ENC] ret=' + r);
        return r;
      };
    });
  } catch (e) {
    console.log('[NS] 失败 ' + e);
  }

  console.log('[uc-netsec-dump] 已安装');
});
