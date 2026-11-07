// 精确采集一次完整的信封生成过程。
//
// UC SDK 生成信封的顺序是：
//   createRequestJson(key, iv)   ← 拿到随机 key/iv
//   encrypt(明文)                ← 用它们加密，返回 {"i","k","d","v"}
//
// 这里把「key / iv / 明文 / 信封」按十六进制或原文成对记下来，
// 供离线复现：用同样的 key/iv 加密同样的明文，密文应当逐字节相同。
// 若相同，说明 AES 那一半完全正确，问题只可能在 RSA 包裹。

Java.perform(function () {
  var JString = Java.use('java.lang.String');

  var lastKey = null;
  var lastIv = null;
  var seq = 0;
  var MAX = 6;

  function hex(bytes) {
    try {
      if (bytes === null) return '';
      var out = [];
      for (var i = 0; i < bytes.length; i++) {
        var v = bytes[i];
        if (v < 0) v += 256;
        out.push(('0' + v.toString(16)).slice(-2));
      }
      return out.join('');
    } catch (e) {
      return 'ERR';
    }
  }

  function textOf(v) {
    try {
      if (v === null) return '';
      if (typeof v === 'string') return v;
      // Java byte[]
      return JString.$new(v, 'UTF-8').toString();
    } catch (e) {
      return '' + v;
    }
  }

  function emit(tag, s) {
    for (var i = 0; i < s.length; i += 200) {
      console.log(tag + '|' + i + '|' + s.substr(i, 200));
    }
  }

  function findClass(name) {
    var loaders = Java.enumerateClassLoadersSync();
    for (var i = 0; i < loaders.length; i++) {
      try {
        var f = Java.ClassFactory.get(loaders[i]);
        var c = f.use(name);
        if (c !== null) return c;
      } catch (e) {}
    }
    return null;
  }

  function install() {
    var Target = findClass('cn.uc.gamesdk.lib.network.protocol.NetworkSecurity');
    if (Target === null) {
      console.log('[NET] 未找到，10 秒后重试');
      setTimeout(install, 10000);
      return;
    }

    // createRequestJson(key, iv) 的参数不是裸 byte[]，改从密钥构造点取：
    // SecretKeySpec 带来 AES key，IvParameterSpec 带来 IV，这两个最可靠。
    try {
      var KeySpec = Java.use('javax.crypto.spec.SecretKeySpec');
      KeySpec.$init.overload('[B', 'java.lang.String').implementation = function (k, alg) {
        try {
          if (k !== null && k.length === 16) lastKey = hex(k);
        } catch (e) {}
        return this.$init(k, alg);
      };
      console.log('[NET] SecretKeySpec 已挂');
    } catch (e) {
      console.log('[NET] 挂 SecretKeySpec 失败: ' + e);
    }

    try {
      var IvSpec = Java.use('javax.crypto.spec.IvParameterSpec');
      IvSpec.$init.overload('[B').implementation = function (iv) {
        try {
          if (iv !== null && iv.length === 16) lastIv = hex(iv);
        } catch (e) {}
        return this.$init(iv);
      };
      console.log('[NET] IvParameterSpec 已挂');
    } catch (e) {
      console.log('[NET] 挂 IvParameterSpec 失败: ' + e);
    }

    try {
      var enc = Target.encrypt.overloads;
      for (var j = 0; j < enc.length; j++) {
        (function (ov) {
          ov.implementation = function () {
            var ret = ov.apply(this, arguments);
            try {
              if (seq < MAX) {
                seq++;
                var plain = textOf(arguments[0]);
                var outText = textOf(ret) || ('' + ret);
                console.log('[PAIR' + seq + '] key=' + lastKey + ' iv=' + lastIv);
                console.log('[PAIR' + seq + '] plainLen=' + plain.length);
                emit('P' + seq, plain);
                console.log('[PAIR' + seq + '] envelope=' + outText.length);
                emit('E' + seq, outText);
              }
            } catch (e) {}
            return ret;
          };
        })(enc[j]);
      }
      console.log('[NET] encrypt 已挂 ' + enc.length + ' 个重载');
    } catch (e) {
      console.log('[NET] 挂 encrypt 失败: ' + e);
    }

    console.log('[uc-pair-dump] 就绪');
  }

  setTimeout(install, 12000);
  console.log('[uc-pair-dump] 已安装');
});
