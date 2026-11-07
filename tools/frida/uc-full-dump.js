// 九游 / UC SDK 全量登录观测脚本
//
// 采集三类关键材料，供协议复刻时对账：
//
//	[KEY]  getSecurityKey 的响应（含服务端下发的 RSA 公钥）
//	[PWD]  账号密码登录的请求与响应（含 password 字段的真实形态）
//	[SID] / [RT] / [ST] / [ACC]  登录令牌与多账号列表
//
// 每次命中都带序号（如 [PWD1]、[PWD2]），便于区分多轮请求。
// 长内容按 200 字符分片，格式 `<标签><序号>|<偏移>|<内容>`。

Java.perform(function () {
  var JString = Java.use('java.lang.String');
  var pwdSeq = 0;
  var keySeq = 0;

  function emit(prefix, seq, s) {
    for (var i = 0; i < s.length; i += 200) {
      console.log(prefix + seq + '|' + i + '|' + s.substr(i, 200));
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

    // 服务端下发的公钥（取一次就够，但每次出现都留着便于对比是否轮换）
    if (s.indexOf('securityKey') >= 0) {
      keySeq++;
      console.log('[KEY' + keySeq + ']');
      emit('K', keySeq, s);
      return;
    }

    // 账号密码登录：请求与响应都留完整一份
    if (s.indexOf('loginByMobilePassword') >= 0) {
      pwdSeq++;
      console.log(isInput ? '[PWDREQ' + pwdSeq + ']' : '[PWDRESP' + pwdSeq + ']');
      emit(isInput ? 'P' : 'Q', pwdSeq, s);
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

  console.log('[uc-full-dump] 已安装');
});
