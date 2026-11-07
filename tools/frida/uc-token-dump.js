// 九游 / UC SDK 登录令牌观测脚本
//
// 用途：只打印登录过程中真正有价值的几个字段（sid / refreshToken /
// serviceTicket / accountList），输出量很小，适合常驻。
//
// 为什么要单独一个脚本：完整 dump 会把日志刷到几万条，翻页都翻不动；
// 而实际需要的只有这几个字段，直接在 hook 里定位并切片输出最省事。

Java.perform(function () {
  var JString = Java.use('java.lang.String');

  function slice(text, index, before, after) {
    var start = Math.max(0, index - before);
    var end = Math.min(index + after, text.length);
    return text.substring(start, end);
  }

  function scan(bytes) {
    try {
      if (bytes === null || bytes.length === 0) return;
      var s = JString.$new(bytes, 'UTF-8').toString();
      var i;

      i = s.indexOf('refreshToken');
      if (i >= 0) console.log('[RT] ' + slice(s, i, 40, 150));

      i = s.indexOf('"sid":"');
      if (i >= 0) console.log('[SID] ' + slice(s, i, 0, 70));

      i = s.indexOf('serviceTicket');
      if (i >= 0) console.log('[ST] ' + slice(s, i, 30, 120));

      i = s.indexOf('accountList');
      if (i >= 0) console.log('[ACC] ' + slice(s, i, 0, 200));
    } catch (e) {}
  }

  try {
    var Cipher = Java.use('javax.crypto.Cipher');
    Cipher.doFinal.overload('[B').implementation = function (input) {
      var out = this.doFinal(input);
      try {
        scan(out);
        scan(input);
      } catch (e) {}
      return out;
    };
  } catch (e) {
    console.log('hook Cipher 失败: ' + e);
  }

  console.log('[uc-token-dump] 已安装');
});
