// 九游 / UC SDK 登录令牌与密码登录观测脚本
//
// 用途：只打印登录过程中真正有价值的字段，输出量小，适合常驻。
//
// 覆盖四类目标：
//   [PWDREQ] / [PWDRESP]  账号密码登录的请求与响应（含手机号、密码是否加密）
//   [SID]                 sid（会话凭证）
//   [RT]                  refreshToken（续期用）
//   [ST] / [ACC]          serviceTicket 与多账号列表
//
// 长内容按 200 字符分片输出，格式 `<标签>|<偏移>|<内容>`；
// 单条日志有长度上限，不分片会丢掉尾部（往往正是 refreshToken 所在位置）。

Java.perform(function () {
  var JString = Java.use('java.lang.String');

  function emit(tag, s) {
    for (var i = 0; i < s.length; i += 200) {
      console.log(tag + '|' + i + '|' + s.substr(i, 200));
    }
  }

  function asText(bytes) {
    try {
      if (bytes === null || bytes.length === 0) return '';
      return JString.$new(bytes, 'UTF-8').toString();
    } catch (e) {
      return '';
    }
  }

  function slice(text, index, before, after) {
    return text.substring(Math.max(0, index - before), Math.min(index + after, text.length));
  }

  function scan(bytes, isInput) {
    var s = asText(bytes);
    if (!s) return;

    // 账号密码登录：请求与响应都完整留一份（分片）
    if (s.indexOf('loginByMobilePassword') >= 0) {
      console.log(isInput ? '[PWDREQ]' : '[PWDRESP]');
      emit(isInput ? 'P' : 'Q', s);
      return;
    }

    if (s.indexOf('refreshToken') >= 0) {
      console.log('[RT] ' + slice(s, s.indexOf('refreshToken'), 40, 150));
    }
    if (s.indexOf('"sid":"') >= 0) {
      console.log('[SID] ' + slice(s, s.indexOf('"sid":"'), 0, 70));
    }
    if (s.indexOf('serviceTicket') >= 0) {
      console.log('[ST] ' + slice(s, s.indexOf('serviceTicket'), 30, 120));
    }
    if (s.indexOf('accountList') >= 0) {
      console.log('[ACC] ' + slice(s, s.indexOf('accountList'), 0, 200));
    }
  }

  try {
    var Cipher = Java.use('javax.crypto.Cipher');
    Cipher.doFinal.overload('[B').implementation = function (input) {
      var out = this.doFinal(input);
      try {
        if (input !== null && input.length > 32) scan(input, true);
        if (out !== null && out.length > 32) scan(out, false);
      } catch (e) {}
      return out;
    };
  } catch (e) {
    console.log('hook Cipher 失败: ' + e);
  }

  console.log('[uc-login-dump] 已安装');
});
