/**
 * 打码工具。
 *
 * 原则：默认全部遮住，只有用户主动点小眼睛才显示明文。
 * 手机号/邮箱属于敏感信息，账号 ID 也算半个敏感信息（能查到人）。
 */

/** 手机号 / 邮箱打码：138****8000、abc***@163.com */
export function maskName(name: string): string {
  if (!name) return "";
  const at = name.indexOf("@");
  if (at > 0) {
    const user = name.slice(0, at);
    const domain = name.slice(at);
    const keep = user.length > 4 ? user.slice(0, 3) : user.slice(0, 1);
    return `${keep}***${domain}`;
  }
  if (/^\d{7,}$/.test(name)) {
    return name.length > 7 ? `${name.slice(0, 3)}****${name.slice(-4)}` : name;
  }
  return name;
}

/** 账号 ID 打码：只留前 3 位和后 3 位 */
export function maskId(id: string): string {
  if (!id) return "";
  if (id.length <= 6) return id;
  return `${id.slice(0, 3)}****${id.slice(-3)}`;
}
