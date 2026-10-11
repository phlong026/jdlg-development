package cn.iyque.phase1;
import cn.iyque.domain.IYQueCustomerInfo;
import org.springframework.data.jpa.repository.*;
import org.springframework.data.repository.query.Param;
import javax.persistence.LockModeType;
import java.time.Instant;
import java.util.*;
import static cn.iyque.phase1.Models.*;

public final class Repositories {
 public interface Customers extends JpaRepository<Customer,String>,JpaSpecificationExecutor<Customer> {
  @Query("select coalesce(c.ownerUsername,c.ownerId), count(c), sum(case when c.stage in ('new','following') and c.nextFollowupAt>=:start and c.nextFollowupAt<:end then 1 else 0 end), sum(case when c.stage in ('new','following') and c.nextFollowupAt<:now then 1 else 0 end), sum(case when c.stage in ('new','following') and c.nextFollowupAt is null then 1 else 0 end) from CrmCustomer c where c.mergedInto is null and (c.ownerUsername is not null or c.ownerId is not null) and c.poolAt is null and (c.protectionExpiresAt is null or c.protectionExpiresAt>:now) group by coalesce(c.ownerUsername,c.ownerId) order by coalesce(c.ownerUsername,c.ownerId)")
  List<Object[]> team(@Param("start") Instant start,@Param("end") Instant end,@Param("now") Instant now);
  @Lock(LockModeType.PESSIMISTIC_WRITE) @Query("select c from CrmCustomer c where c.id=:id")
  Optional<Customer> locked(@Param("id") String id);
  List<Customer> findByPhoneKeyOrPhone(String phoneKey,String phone);
  @Lock(LockModeType.PESSIMISTIC_WRITE)
  @Query("select c from CrmCustomer c where c.mergedInto is null and c.lifecycle = 'protected' and c.protectionExpiresAt <= :now and c.poolAt is null order by c.id")
  List<Customer> expired(@Param("now") Instant now, org.springframework.data.domain.Pageable page);
 }
 public interface Relations extends JpaRepository<IYQueCustomerInfo,String> {
  List<IYQueCustomerInfo> findByExternalUserid(String id);
  List<IYQueCustomerInfo> findByUserId(String id);
 }
 public interface Followups extends JpaRepository<Followup,Long> {boolean existsByExternalKey(String externalKey);
  List<Followup> findByCustomerIdOrderByCreatedAtDescIdDesc(String id);
 }
 public interface Employees extends JpaRepository<Employee,String> {}
 public interface Accounts extends JpaRepository<Account,String> {
  @Lock(LockModeType.PESSIMISTIC_WRITE) @Query("select a from CrmAccount a where a.username=:username") Optional<Account> locked(@Param("username") String username);
 }
 public interface Sources extends JpaRepository<Source,String> {
  List<Source> findByCustomerId(String id);
  List<Source> findByOriginCustomerId(String id);
 }
 public interface Attachments extends JpaRepository<Attachment,String> {List<Attachment> findByCustomerIdOrderByCreatedAtDesc(String id);long countByCustomerId(String id);}
 public interface Cases extends JpaRepository<ReviewCase,Long> {boolean existsByStatusAndApplicantAndKindAndCustomerId(String status,String applicant,String kind,String customerId);List<ReviewCase> findByCustomerIdOrderByCreatedAtDesc(String id);List<ReviewCase> findTop200ByStatusOrderByCreatedAtAsc(String status);}
 public interface Notices extends JpaRepository<Notice,String> {List<Notice> findTop100ByUsernameOrderByCreatedAtDesc(String username);
  List<Notice> findTop20ByPushStateOrderByCreatedAtAsc(String state);
  @org.springframework.transaction.annotation.Transactional @Modifying @Query("update CrmNotice n set n.pushState='sending' where n.id=:id and n.pushState='pending'") int claimPush(@Param("id") String id);
  @org.springframework.transaction.annotation.Transactional @Modifying @Query("update CrmNotice n set n.pushState=:state,n.pushError=:error where n.id=:id and n.pushState='sending'") int finishPush(@Param("id") String id,@Param("state") String state,@Param("error") String error);
  @org.springframework.transaction.annotation.Transactional @Modifying @Query("update CrmNotice n set n.pushState='pending',n.pushError='' where n.id=:id and n.pushState='failed'") int retryPush(@Param("id") String id);
}
 public interface Sessions extends JpaRepository<Session,String> {
  void deleteByUsername(String username);
  void deleteByExpiresAtBefore(Instant instant);
 }
 public interface Events extends JpaRepository<Event,String> {
  List<Event> findTop30ByStatusAndNextAttemptAtBeforeOrderByEventTimeAsc(String status,Instant now);
  long countByStatus(String status);
  List<Event> findTop50ByStatusNotOrderByCreatedAtDesc(String status);
 }
 public interface Settings extends JpaRepository<Setting,String> {
  @Lock(LockModeType.PESSIMISTIC_WRITE) @Query("select s from CrmSetting s where s.id=:id") Optional<Setting> locked(@Param("id") String id);
 }
 public interface SyncRuns extends JpaRepository<SyncRun,Long> {
  List<SyncRun> findTop10ByOrderByIdDesc();
  Optional<SyncRun> findFirstByStatusOrderByIdAsc(String status);
 }
}
