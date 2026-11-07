// 只抓 getSecurityKey 的完整明文，用于与本地实现逐字段对比。
//
// 这个请求在启动阶段就会发，体积不到 1 KB，最容易对齐。

Java.perform(function () {
  var JString = Java.use('java.lang.String');
  var seq = 0;
  var MAX = 3;

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
        if (input === null || input.length < 100 || seq >= MAX) return out;
        var s = JString.$new(input, 'UTF-8').toString();
        if (s.indexOf('system.getSecurityKey') >= 0) {
          seq++;
          console.log('[GSK' + seq + '] len=' + input.length);
          emit('G' + seq, s);
        }
      } catch (e) {}
      return out;
    };
    console.log('[uc-gsk-dump] 已安装');
  } catch (e) {
    console.log('[uc-gsk-dump] 安装失败: ' + e);
  }
});
