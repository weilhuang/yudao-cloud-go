#!/bin/sh
# 只给本机开发用。已有文件时直接退出，避免覆盖正在使用的密钥。
set -eu

target=${1:-.env}
if [ -e "$target" ]; then
  echo "配置文件已存在：$target" >&2
  exit 1
fi

umask 077
mysql_password=$(openssl rand -hex 16)
encryptor_password=$(openssl rand -hex 16)
cat > "$target" <<EOF
# 本机开发专用。不要提交或复用到线上。
YUDAO_DEV_MYSQL_PASSWORD=$mysql_password
YUDAO_MYSQL_DSN='root:$mysql_password@tcp(127.0.0.1:3306)/ruoyi-vue-pro?charset=utf8mb4&parseTime=True&loc=Local&timeout=3s'
YUDAO_MYBATIS_ENCRYPTOR_PASSWORD=$encryptor_password
EOF
echo "已生成 ${target}（仅当前用户可读写）。"
