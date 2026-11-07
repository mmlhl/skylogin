// 九游 / UC SDK 信封入口观测脚本
//
// 目标：`NetworkSecurity.encrypt / decrypt` 是 SDK 生成与解析信封的统一入口。
// hook 它能直接看到「明文 ↔ k/i/d」的对应关系，用来对齐我们自己的实现。
//
// UC SDK 的类由运行时下载的 dex 加载（默认 ClassLoader 找不到），
// 所以这里先遍历已加载的 ClassLoader，再用能命中的那个去 use。

Java.perform(function () {
  var JString = Java.use('java.lang.String');
  var seq = 0;

  function asText(bytes) {
    try {
      if (bytes === null || bytes.length === 0) return '';
      return JString.$new(bytes, 'UTF-8').toString();
    } catch (e) {
      return '';
    }
  }

  function emit(prefix, n, s) {
    for (var i = 0; i < s.length; i += 200) {
      console.log(prefix + n + '|' + i + '|' + s.substr(i, 200));
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

  function hookAll() {
    var Target = findClass('cn.uc.gamesdk.lib.network.protocol.NetworkSecurity');
    if (Target === null) {
      console.log('[NET] 没找到 NetworkSecurity，10 秒后重试');
      setTimeout(hookAll, 10000);
      return;
    }
    console.log('[NET] 已定位 NetworkSecurity');

    var methods = Target.class.getDeclaredMethods();
    var names = [];
    for (var i = 0; i < methods.length; i++) names.push(methods[i].getName());
    console.log('[NET] 方法: ' + names.join(','));

    // encrypt(Object) / decrypt(String) 的具体签名要在运行时确认，
    // 这里按重载数量逐个尝试挂载，挂不上的跳过。
    var hookOne = function (mName, tag) {
      try {
        var overloads = Target[mName].overloads;
        for (var k = 0; k < overloads.length; k++) {
          (function (ov) {
            ov.implementation = function () {
              var args = [];
              for (var a = 0; a < arguments.length; a++) {
                var v = arguments[a];
                args.push(v === null ? 'null' : ('' + v));
              }
              var ret = ov.apply(this, arguments);
              seq++;
              console.log('[' + tag + seq + '] 入参 ' + args.join(' || ').substring(0, 200));
              var out = '' + ret;
              emit(tag === 'ENC' ? 'E' : 'D', seq, out);
              return ret;
            };
          })(overloads[k]);
        }
        console.log('[NET] 已挂 ' + mName + ' 共 ' + overloads.length + ' 个重载');
      } catch (e) {
        console.log('[NET] 挂 ' + mName + ' 失败: ' + e);
      }
    };

    hookOne('encrypt', 'ENC');
    hookOne('decrypt', 'DEC');
    hookOne('createRequestJson', 'REQ');
  }

  // 类要等 SDK 初始化后才加载
  setTimeout(hookAll, 12000);
  console.log('[uc-net-dump] 已安装');
});
