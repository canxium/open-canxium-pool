#!/bin/sh

# start redis-server
mkdir /opt/redis
echo 'appendonly yes' | redis-server --dir /opt/redis - &
rm -rf /etc/nginx/sites-enabled/default
rm -rf /etc/nginx/sites-available/default
cp /app/www/default.conf /etc/nginx/sites-available/default
ln -s /etc/nginx/sites-available/default /etc/nginx/sites-enabled/default
nginx
nginx -s reload
/out/main api.json
