#!/bin/bash
sudo apt update
sudo apt install docker.io docker-compose -y
sudo usermod -aG docker $USER

git clone https://github.com/ambareesh1510/openaimtrainer-backend

cd openaimtrainer-backend
sudo docker-compose up -d
