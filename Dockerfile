FROM ubuntu:22.04
RUN apt update
RUN apt install software-properties-common -y
RUN add-apt-repository ppa:longsleep/golang-backports
RUN apt update
RUN apt install golang-go -y
RUN go version
RUN apt install lsb-release curl gpg -y
RUN curl -fsSL https://packages.redis.io/gpg | gpg --dearmor -o /usr/share/keyrings/redis-archive-keyring.gpg
RUN echo "deb [signed-by=/usr/share/keyrings/redis-archive-keyring.gpg] https://packages.redis.io/deb $(lsb_release -cs) main" | tee /etc/apt/sources.list.d/redis.list
RUN apt update
RUN apt-get install redis -y
RUN apt install git -y

RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash - && apt-get install -y nodejs
RUN apt install nginx -y

RUN npm install -g ember-cli@2.18
RUN npm install -g bower

RUN mkdir -p /modules
COPY www /modules
RUN cd /modules && npm install && bower install && ember install ember-truth-helpers && npm install jdenticon@2.1.0
RUN mkdir -p /app/www && cp -a /modules/node_modules /app/www

WORKDIR /app
COPY . /app
RUN go mod download
RUN go build  -o /out/main ./
RUN cd /app/www && bower install && chmod a+x build.sh && ./build.sh
EXPOSE 80/tcp
EXPOSE 8008/tcp
RUN chmod a+x /app/entrypoint.sh
ENTRYPOINT ["/app/entrypoint.sh"]
