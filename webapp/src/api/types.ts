export interface ApiResponse<T = unknown> {
  code: number;
  message: string;
  data?: T;
}

export interface Plan {
  id: string;
  name: string;
  description: string;
  price: number;
  period: string;
  features: string[];
}

export interface Order {
  id: string;
  planId: string;
  period: string;
  amount: number;
  status: string;
  createdAt: string;
}

export interface Device {
  id: string;
  name: string;
  remark: string;
  platform: string;
  lastActiveAt: string;
  createdAt: string;
}

export interface Member {
  id: string;
  email: string;
  role: string;
  status: string;
  createdAt: string;
}

export interface InviteCode {
  id: string;
  code: string;
  remark: string;
  used: boolean;
  usedBy: string | null;
  createdAt: string;
}

export interface Issue {
  id: string;
  title: string;
  content: string;
  status: string;
  createdAt: string;
  updatedAt: string;
}

export interface Comment {
  id: string;
  issueId: string;
  content: string;
  author: string;
  createdAt: string;
}

export interface AppConfig {
  version: string;
  downloadUrl: string;
  features: Record<string, boolean>;
}

export interface ProHistory {
  id: string;
  planName: string;
  startAt: string;
  endAt: string;
  status: string;
}
