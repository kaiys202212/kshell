// 合并 className 的小工具：clsx 组合 + tailwind-merge 去重冲突类
import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
