import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // 刻意【不声明 env】—— next.config 的 env 会把变量内联进客户端 bundle。
  // MELETE_API_BASE 指向内网 ClusterIP（生产: http://melete-api:8080/api/v1），
  // 只该被服务端读到；用 process.env 直接读即可，浏览器无从得知它的存在。
  output: "standalone", // 容器镜像用：只打包运行时真正需要的文件
};

export default nextConfig;
