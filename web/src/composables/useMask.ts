/**
 * 打码工具。
 *
 * 原则：默认全部遮住，只有用户主动点小眼睛才显示明文。
 * 手机号/邮箱属于敏感信息，账号 ID 也算半个敏感信息（能查到人）。
 */

/** 手机号 / 邮箱打码：138****8000、abc***@163.com */
export function maskName(name: string): string {
  if (!name) return "";
  // 邮箱可能只是名字的一部分（如「abc@163.com · iOS 区」），就地打码。
  let out = name.replace(
    /([A-Za-z0-9._%+-]+)@([A-Za-z0-9.-]+\.[A-Za-z]{2,})/g,
    (_all: string, user: string, domain: string) => {
      const keep = user.length > 4 ? user.slice(0, 3) : user.slice(0, 1);
      return `${keep}***@${domain}`;
    },
  );
  // 手机号同理：副本/别名会带「 · iOS 区」这类后缀，不能只认整串纯数字。
  out = out.replace(/\d{7,}/g, (digits: string) =>
    digits.length > 7 ? `${digits.slice(0, 3)}****${digits.slice(-4)}` : digits,
  );
  return out;
}

/** 账号 ID 打码：只留前 3 位和后 3 位 */
export function maskId(id: string): string {
  if (!id) return "";
  if (id.length <= 6) return id;
  return `${id.slice(0, 3)}****${id.slice(-3)}`;
}
