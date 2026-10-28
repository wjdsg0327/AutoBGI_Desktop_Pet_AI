// Go 零时间按秒与纳秒共同判断，不能让 Date 的毫秒精度吞掉有效的纳秒事实。
const goZeroMilliseconds = (() => {
  const date = new Date(0)
  date.setUTCFullYear(1, 0, 1)
  return date.getTime()
})()

// 对齐存储层 Go 1.26 RFC3339Nano 的兼容解析；保留事实原文，只返回用于展示的时间。
export function parseNotificationTime(value: unknown): Date | null {
  if (typeof value !== 'string' || value.length > 64) return null
  const parts = /^([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{1,2}):([0-9]{2}):([0-9]{2})(?:[.,]([0-9]+))?(?:Z|([+-])([0-9]{2}):([0-9]{2}))$/.exec(value)
  if (!parts || parts[0].length !== value.length) return null
  const [year, month, day, hour, minute, second] = parts.slice(1, 7).map(Number)
  const zoneHour = Number(parts[9] || 0)
  const zoneMinute = Number(parts[10] || 0)
  const leap = year! % 4 === 0 && (year! % 100 !== 0 || year! % 400 === 0)
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]
  if (year! < 1 || month! < 1 || month! > 12 || day! < 1 || day! > days[month! - 1]! ||
    hour! > 23 || minute! > 59 || second! > 59 || zoneHour > 24 || zoneMinute > 60) return null

  // 先按局部日历构造整秒，再独立应用偏移；setUTCFullYear 避免 1..99 年被当成 1901..1999。
  const calendar = new Date(0)
  calendar.setUTCFullYear(year!, month! - 1, day!)
  calendar.setUTCHours(hour!, minute!, second!, 0)
  const offset = (zoneHour * 60 + zoneMinute) * (parts[8] === '-' ? -1 : 1)
  const wholeSecond = calendar.getTime() - offset * 60000
  // Go 接受逗号和超过九位的小数，并直接截断；零时间判断必须在毫秒转换之前完成。
  const nanoseconds = Number((parts[7] || '').slice(0, 9).padEnd(9, '0'))
  if (wholeSecond === goZeroMilliseconds && nanoseconds === 0) return null
  return new Date(wholeSecond + Math.floor(nanoseconds / 1000000))
}
