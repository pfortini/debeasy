-- Grant the dev user full server privileges so the SQL editor / create-db wizard works
-- against the local MySQL container. Only intended for the docker-compose dev setup.
GRANT ALL PRIVILEGES ON *.* TO 'debeasy'@'%' WITH GRANT OPTION;
FLUSH PRIVILEGES;
