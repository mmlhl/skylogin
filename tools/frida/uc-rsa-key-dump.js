// 目的：拿到 SDK 真正用于 RSA 加密的 Key 对象本体。
//
// 此前的验证只 hook 到了算法名与输入输出长度，
// "SDK 用的公钥 == getSecurityKey 下发的 K2" 一直是推断而非实测。
// 本脚本把三样东西全部打出来，供 PC 端逐字节比对：
//   1. KeyFactory.generatePublic 的输入 DER —— SDK 从哪个字节串解析出公钥
//   2. Cipher.init 收到的 Key.getEncoded() —— 真正参与加密的那把
//   3. doFinal 的输入密文/输出字节 —— 送进 RSA 的 16 字节究竟是什么
//
// 设备需处于"已进入九游登录"状态才会触发。

Java.perform(function () {
  var Base64 = Java.use('android.util.Base64'); // NO_WRAP = 2

  function b64(bytes) {
    try {
      if (bytes === null || bytes === undefined) return 'null';
      return Base64.encodeToString(bytes, 2);
    } catch (e) {
      return 'ERR(' + e + ')';
    }
  }

  function hex(bytes, limit) {
    try {
      if (bytes === null || bytes === undefined) return 'null';
      var n = limit ? Math.min(bytes.length, limit) : bytes.length;
      var out = [];
      for (var i = 0; i < n; i++) {
        var v = bytes[i];
        if (v < 0) v += 256;
        out.push(('0' + v.toString(16)).slice(-2));
      }
      return out.join('');
    } catch (e) {
      return 'ERR';
    }
  }

  // 公钥的 DER 导出。OpenSSLRSAPublicKey 在部分版本上不暴露 getModulus，
  // 但 getEncoded() 走的是 X.509 SubjectPublicKeyInfo，各实现都支持。
  function dumpKey(tag, key) {
    if (!key) {
      console.log(tag + ' key=null');
      return;
    }
    try {
      console.log(tag + ' algo=' + key.getAlgorithm() + ' format=' + key.getFormat());
    } catch (e) {
      console.log(tag + ' 基本信息失败 ' + e);
    }
    try {
      var enc = key.getEncoded();
      console.log(tag + ' DER=' + b64(enc));
    } catch (e) {
      console.log(tag + ' getEncoded 失败 ' + e);
    }
    // 兜底：直接反射拿模数与指数
    try {
      var cls = key.getClass();
      var m = cls.getMethod('getModulus', null);
      console.log(tag + ' MOD=' + b64(m.invoke(key, null).toByteArray()));
    } catch (e) {
      // 正常，多数实现没有这条路径
    }
  }

  // ---- 1. 公钥从哪来 ----
  try {
    var KF = Java.use('java.security.KeyFactory');
    KF.generatePublic.overload('java.security.spec.KeySpec').implementation = function (spec) {
      var k = this.generatePublic(spec);
      try {
        if (this.getAlgorithm().toUpperCase().indexOf('RSA') >= 0) {
          console.log('[KF] generatePublic');
          dumpKey('[KF]', k);
          try {
            console.log('[KF] SPEC-DER=' + b64(spec.getEncoded()));
          } catch (e) {}
        }
      } catch (e) {}
      return k;
    };
  } catch (e) {
    console.log('KeyFactory hook 失败: ' + e);
  }

  var Cipher = Java.use('javax.crypto.Cipher');

  // ---- 2. 算法与 provider ----
  try {
    Cipher.getInstance.overload('java.lang.String').implementation = function (t) {
      var c = this.getInstance(t);
      try {
        if (t && t.toUpperCase().indexOf('RSA') >= 0) {
          console.log('[GET] ' + t + ' provider=' + c.getProvider().getName());
        }
      } catch (e) {}
      return c;
    };
  } catch (e) {
    console.log('getInstance hook 失败: ' + e);
  }

  // ---- 3. 真正参与加密的 Key ----
  function hookInit(sig, label) {
    try {
      Cipher.init.overload.apply(Cipher.init, sig).implementation = function () {
        var r = this.init.apply(this, arguments);
        try {
          var algo = this.getAlgorithm() || '';
          if (algo.toUpperCase().indexOf('RSA') >= 0) {
            var key = arguments[1];
            console.log('[INIT' + label + '] ' + algo + ' mode=' + arguments[0]);
            dumpKey('[INIT' + label + ']', key);
          }
        } catch (e) {}
        return r;
      };
    } catch (e) {
      console.log('init' + label + ' hook 失败: ' + e);
    }
  }
  hookInit(['int', 'java.security.Key'], '');
  hookInit(['int', 'java.security.Key', 'java.security.SecureRandom'], 'R');
  hookInit(['int', 'java.security.Key', 'java.security.spec.AlgorithmParameterSpec'], 'P');

  // ---- 4. 送入 RSA 的明文与产出 ----
  function reportDoFinal(algo, input, out) {
    if (!algo || algo.toUpperCase().indexOf('RSA') < 0) return;
    var inLen = input === null || input === undefined ? 0 : input.length;
    var outLen = out === null || out === undefined ? 0 : out.length;
    console.log('[RSA] ' + algo + ' in=' + inLen + ' out=' + outLen);
    console.log('[RSA] IN-HEX =' + hex(input, 64));
    console.log('[RSA] IN-B64 =' + b64(input));
    console.log('[RSA] OUT-HEX=' + hex(out, 8) + '…');
    console.log('[RSA] OUT-B64=' + b64(out));
  }

  var doFinalSigs = [
    ['[B', function (input) {
      var out = this.doFinal(input);
      try { reportDoFinal(this.getAlgorithm(), input, out); } catch (e) {}
      return out;
    }],
    ['[B', 'int', 'int', function (input, off, len) {
      var out = this.doFinal(input, off, len);
      try { reportDoFinal(this.getAlgorithm(), input, out); } catch (e) {}
      return out;
    }],
    [null, function () {
      var out = this.doFinal();
      try { reportDoFinal(this.getAlgorithm(), null, out); } catch (e) {}
      return out;
    }]
  ];
  doFinalSigs.forEach(function (item) {
    var impl = item[item.length - 1];
    var sig = item.slice(0, -1).filter(function (x) { return x !== null; });
    try {
      if (sig.length === 0) {
        Cipher.doFinal.overload().implementation = impl;
      } else {
        Cipher.doFinal.overload.apply(Cipher.doFinal, sig).implementation = impl;
      }
    } catch (e) {
      console.log('doFinal hook 失败 ' + sig.join(',') + ': ' + e);
    }
  });

  console.log('[uc-rsa-key-dump] 已安装');
});
