export const rand = (min: number, max: number) => min + Math.random() * (max - min)

export const randInt = (min: number, max: number) => Math.floor(rand(min, max + 1))

export const pick = <T>(list: readonly T[]): T => list[Math.floor(Math.random() * list.length)]

export const hex = (length: number) =>
  Array.from({ length }, () => Math.floor(Math.random() * 16).toString(16)).join('')

export const clamp = (value: number, min: number, max: number) => Math.max(min, Math.min(max, value))
